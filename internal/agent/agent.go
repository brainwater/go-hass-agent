// Copyright (c) 2023 Joshua Rich <joshua.rich@gmail.com>
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package agent

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/joshuar/go-hass-agent/internal/agent/config"
	"github.com/joshuar/go-hass-agent/internal/agent/ui"
	fyneui "github.com/joshuar/go-hass-agent/internal/agent/ui/fyneUI"
	"github.com/joshuar/go-hass-agent/internal/device"
	"github.com/joshuar/go-hass-agent/internal/hass/api"
	"github.com/joshuar/go-hass-agent/internal/scripts"
	"github.com/joshuar/go-hass-agent/internal/tracker"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Agent holds the data and structure representing an instance of the agent.
// This includes the data structure for the UI elements and tray and some
// strings such as app name and version.
type Agent struct {
	ui      ui.AgentUI
	config  config.AgentConfig
	sensors *tracker.SensorTracker
	done    chan struct{}
	options *AgentOptions
}

// AgentOptions holds options taken from the command-line that was used to
// invoke go-hass-agent that are relevant for agent functionality.
type AgentOptions struct {
	ID                 string
	Headless, Register bool
}

func newAgent(o *AgentOptions) *Agent {
	var err error
	var configPath = filepath.Join(os.Getenv("HOME"), ".config", o.ID)
	a := &Agent{
		done:    make(chan struct{}),
		options: o,
	}
	a.ui = fyneui.NewFyneUI(a)
	if err = config.UpgradeConfig(configPath); err != nil {
		if _, ok := err.(*config.ConfigFileNotFoundError); !ok {
			log.Fatal().Err(err).Msg("Could not upgrade config.")
		}
	}
	if a.config, err = config.New(configPath); err != nil {
		log.Fatal().Err(err).Msg("Could not open config.")
	}
	a.setupLogging()
	return a
}

// Run is the "main loop" of the agent. It sets up the agent, loads the config
// then spawns a sensor tracker and the workers to gather sensor data and
// publish it to Home Assistant
func Run(options AgentOptions) {
	var wg sync.WaitGroup
	var ctx context.Context
	var cancelFunc context.CancelFunc
	var err error

	agent := newAgent(&options)

	// Pre-flight: check if agent is registered. If not, run registration flow.
	var regWait sync.WaitGroup
	regWait.Add(1)
	go func() {
		defer regWait.Done()
		agent.registrationProcess(context.Background(), "", "", options.Register, options.Headless)
	}()

	// Pre-flight: validate config and use to populate context values.
	var cfgWait sync.WaitGroup
	cfgWait.Add(1)
	go func() {
		defer cfgWait.Done()
		// Wait until registration check is done
		regWait.Wait()
		if err = config.ValidateConfig(agent.config); err != nil {
			log.Fatal().Err(err).Msg("Could not validate config.")
		}
		log.Trace().Msg("Config validation done.")
		if err = agent.SetConfig(config.PrefVersion, agent.AppVersion()); err != nil {
			log.Warn().Err(err).Msg("Unable to set config version to app version.")
		}
		ctx, cancelFunc = agent.setupContext()
	}()

	// Start main work goroutines
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Wait until the config is validated and context is set up
		cfgWait.Wait()

		if agent.sensors, err = tracker.NewSensorTracker(agent); err != nil {
			log.Fatal().Err(err).Msg("Could not start.")
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			agent.startWorkers(ctx)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			agent.runScripts(ctx)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			agent.runNotificationsWorker(ctx, options)
		}()
	}()

	go func() {
		<-agent.done
		log.Debug().Msg("Agent done.")
		cancelFunc()
	}()

	agent.handleSignals()

	agent.ui.DisplayTrayIcon(agent)
	agent.ui.Run()
	wg.Wait()
}

// Register runs a registration flow. It either prompts the user for needed
// information or parses what is already provided. It will send a registration
// request to Home Assistant and handles the response. It will handle either a
// UI or non-UI registration flow.
func Register(options AgentOptions, server, token string) {
	agent := newAgent(&options)
	defer close(agent.done)

	var regWait sync.WaitGroup
	regWait.Add(1)
	go func() {
		defer regWait.Done()
		agent.registrationProcess(context.Background(), server, token, options.Register, options.Headless)
	}()

	go func() {
		<-agent.done
		log.Debug().Msg("Agent done.")
	}()

	agent.handleSignals()

	agent.ui.DisplayTrayIcon(agent)
	agent.ui.Run()

	regWait.Wait()
	log.Info().Msg("Device registered with Home Assistant.")
}

func ShowVersion() {
	log.Info().Msgf("%s: %s", config.AppName, config.AppVersion)
}

func ShowInfo(options AgentOptions) {
	agent := newAgent(&options)
	var info strings.Builder
	var deviceName, deviceID string
	if err := agent.GetConfig(config.PrefDeviceName, &deviceName); err == nil && deviceName != "" {
		fmt.Fprintf(&info, "Device Name %s.", deviceName)
	}
	if err := agent.GetConfig(config.PrefDeviceID, &deviceID); err == nil && deviceID != "" {
		fmt.Fprintf(&info, "Device ID %s.", deviceID)
	}
	log.Info().Msg(info.String())
}

// setupLogging will attempt to create and then write logging to a file. If it
// cannot do this, logging will only be available on stdout
func (agent *Agent) setupLogging() {
	logFile, err := agent.config.StoragePath("go-hass-app.log")
	if err != nil {
		log.Error().Err(err).
			Msg("Unable to create a log file. Will only write logs to stdout.")
	} else {
		logWriter, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0664)
		if err != nil {
			log.Error().Err(err).
				Msg("Unable to open log file for writing.")
		} else {
			consoleWriter := zerolog.ConsoleWriter{Out: os.Stdout}
			multiWriter := zerolog.MultiLevelWriter(consoleWriter, logWriter)
			log.Logger = log.Output(multiWriter)
		}
	}
}

func (agent *Agent) setupContext() (context.Context, context.CancelFunc) {
	SharedConfig := &api.APIConfig{}
	if err := agent.config.Get(config.PrefAPIURL, &SharedConfig.APIURL); err != nil {
		log.Fatal().Err(err).Msg("Could not export apiURL.")
	}
	if err := agent.config.Get(config.PrefSecret, &SharedConfig.Secret); err != nil && SharedConfig.Secret != "NOTSET" {
		log.Debug().Err(err).Msg("Could not export secret.")
	}
	ctx := api.NewContext(context.Background(), SharedConfig)
	return context.WithCancel(ctx)
}

// handleSignals will handle Ctrl-C of the agent
func (agent *Agent) handleSignals() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		defer close(agent.done)
		<-c
		log.Debug().Msg("Ctrl-C pressed.")
	}()
}

// Agent satisfies ui.Agent, tracker.Agent and api.Agent interfaces

func (agent *Agent) IsHeadless() bool {
	return agent.options.Headless
}

func (agent *Agent) AppName() string {
	return config.AppName
}

func (agent *Agent) AppID() string {
	return agent.options.ID
}

func (agent *Agent) AppVersion() string {
	return config.AppVersion
}

func (agent *Agent) Stop() {
	log.Debug().Msg("Stopping agent.")
	close(agent.done)
}

func (agent *Agent) GetConfig(key string, value interface{}) error {
	return agent.config.Get(key, value)
}

func (agent *Agent) SetConfig(key string, value interface{}) error {
	return agent.config.Set(key, value)
}

func (agent *Agent) StoragePath(path string) (string, error) {
	return agent.config.StoragePath(path)
}

func (agent *Agent) SensorList() []string {
	return agent.sensors.SensorList()
}

func (agent *Agent) SensorValue(id string) (tracker.Sensor, error) {
	return agent.sensors.Get(id)
}

// StartWorkers will call all the sensor worker functions that have been defined
// for this device.
func (agent *Agent) startWorkers(ctx context.Context) {
	wokerFuncs := sensorWorkers()
	wokerFuncs = append(wokerFuncs, device.ExternalIPUpdater)
	d := newDevice(ctx)
	workerCtx := d.Setup(ctx)

	workerCh := make(chan func(context.Context, device.SensorTracker), len(wokerFuncs))

	for i := 0; i < len(workerCh); i++ {
		workerCh <- wokerFuncs[i]
	}

	var wg sync.WaitGroup
	for _, workerFunc := range wokerFuncs {
		wg.Add(1)
		go func(workerFunc func(context.Context, device.SensorTracker)) {
			defer wg.Done()
			workerFunc(workerCtx, agent.sensors)
		}(workerFunc)
	}

	close(workerCh)
	wg.Wait()
}

func (agent *Agent) runScripts(ctx context.Context) {
	scriptPath, err := agent.config.StoragePath("scripts")
	if err != nil {
		log.Error().Err(err).Msg("Could not retrieve script path from config.")
		return
	}
	allScripts, err := scripts.FindScripts(scriptPath)
	if err != nil || len(allScripts) == 0 {
		log.Error().Err(err).Msg("Could not find any script files.")
		return
	}
	c := cron.New()
	var outCh []<-chan tracker.Sensor
	for _, s := range allScripts {
		schedule := s.Schedule()
		if schedule != "" {
			_, err := c.AddJob(schedule, s)
			if err != nil {
				log.Warn().Err(err).Str("script", s.Path()).
					Msg("Unable to schedule script.")
				break
			}
			outCh = append(outCh, s.Output)
			log.Debug().Str("schedule", schedule).Str("script", s.Path()).
				Msg("Added script sensor.")
		}
	}
	log.Debug().Msg("Starting cron scheduler for script sensors.")
	c.Start()
	go func() {
		for s := range mergeSensorCh(ctx, outCh...) {
			if err := agent.sensors.UpdateSensors(ctx, s); err != nil {
				log.Error().Err(err).Msg("Could not update script sensor.")
			}
		}
	}()
	<-ctx.Done()
	log.Debug().Msg("Stopping cron scheduler for script sensors.")
	cronCtx := c.Stop()
	<-cronCtx.Done()
}

func mergeSensorCh(ctx context.Context, sensorCh ...<-chan tracker.Sensor) <-chan tracker.Sensor {
	var wg sync.WaitGroup
	out := make(chan tracker.Sensor)

	// Start an output goroutine for each input channel in sensorCh.  output
	// copies values from c to out until c is closed, then calls wg.Done.
	output := func(c <-chan tracker.Sensor) {
		defer wg.Done()
		for n := range c {
			select {
			case out <- n:
			case <-ctx.Done():
				return
			}
		}
	}
	wg.Add(len(sensorCh))
	for _, c := range sensorCh {
		go output(c)
	}

	// Start a goroutine to close out once all the output goroutines are
	// done.  This must start after the wg.Add call.
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}
