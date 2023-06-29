// Copyright (c) 2023 Joshua Rich <joshua.rich@gmail.com>
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package agent

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/grandcat/zeroconf"
	"github.com/joshuar/go-hass-agent/internal/hass"
	"github.com/rs/zerolog/log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/data/validation"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	validate "github.com/go-playground/validator/v10"
)

// newRegistration creates a hass.RegistrationDetails object that contains
// information about both the Home Assistant server and the device running the
// agent needed to register the agent with Home Assistant.
func (agent *Agent) newRegistration(ctx context.Context, server, token string) *hass.RegistrationDetails {
	registrationInfo := &hass.RegistrationDetails{
		Server: binding.NewString(),
		Token:  binding.NewString(),
		UseTLS: binding.NewBool(),
		Device: agent.setupDevice(ctx),
	}
	u, err := url.Parse(server)
	if err != nil {
		log.Warn().Err(err).
			Msg("Cannot parse provided URL. Ignoring")
	} else {
		registrationInfo.Server.Set(u.Host)
		if u.Scheme == "https" {
			registrationInfo.UseTLS.Set(true)
		} else {
			registrationInfo.UseTLS.Set(false)
		}
	}
	if token != "" {
		registrationInfo.Token.Set(token)
	}
	return registrationInfo
}

// registrationWindow displays a UI to prompt the user for the details needed to
// complete registration. It will populate with any values that were already
// provided via the command-line.
func (agent *Agent) registrationWindow(ctx context.Context, registration *hass.RegistrationDetails, done chan struct{}) {
	s := findServers(ctx)
	allServers, _ := s.Get()

	w := agent.app.NewWindow(translator.Translate("App Registration"))

	tokenSelect := widget.NewEntryWithData(registration.Token)
	tokenSelect.Validator = validation.NewRegexp("[A-Za-z0-9_\\.]+", "Invalid token format")

	autoServerSelect := widget.NewSelect(allServers, func(s string) {
		registration.Server.Set(s)
	})

	manualServerEntry := widget.NewEntryWithData(registration.Server)
	manualServerEntry.Validator = newHostPort()
	manualServerEntry.Disable()
	manualServerSelect := widget.NewCheck("", func(b bool) {
		switch b {
		case true:
			manualServerEntry.Enable()
			autoServerSelect.Disable()
		case false:
			manualServerEntry.Disable()
			autoServerSelect.Enable()
		}
	})

	tlsSelect := widget.NewCheckWithData("", registration.UseTLS)

	form := widget.NewForm(
		widget.NewFormItem(translator.Translate("Token"), tokenSelect),
		widget.NewFormItem(translator.Translate("Auto-discovered Servers"), autoServerSelect),
		widget.NewFormItem(translator.Translate("Use Custom Server?"), manualServerSelect),
		widget.NewFormItem(translator.Translate("Manual Server Entry"), manualServerEntry),
		widget.NewFormItem(translator.Translate("Use TLS?"), tlsSelect),
	)
	form.OnSubmit = func() {
		s, _ := registration.Server.Get()
		log.Debug().Caller().
			Msgf("User selected server %s", s)

		w.Close()
	}
	form.OnCancel = func() {
		registration = nil
		w.Close()
		ctx.Done()
	}

	w.SetContent(container.New(layout.NewVBoxLayout(),
		widget.NewLabel(
			translator.Translate(
				"As an initial step, this app will need to log into your Home Assistant server and register itself.\nPlease enter the relevant details for your Home Assistant server url/port and a long-lived access token.")),
		form,
	))

	w.SetOnClosed(func() {
		registration = nil
		close(done)
	})

	// w.SetMaster()
	w.Show()
	w.Close()
}

// saveRegistration stores the relevant information from the registration
// request and the successful response in the agent preferences. This includes,
// most importantly, details on the URL that should be used to send subsequent
// requests to Home Assistant.
func (agent *Agent) saveRegistration(r *hass.RegistrationResponse, h *hass.RegistrationDetails) {
	host, _ := h.Server.Get()
	useTLS, _ := h.UseTLS.Get()
	agent.SetPref("Host", host)
	agent.SetPref("UseTLS", useTLS)
	token, _ := h.Token.Get()
	agent.SetPref("Token", token)
	agent.SetPref("Version", agent.Version)
	if r.CloudhookURL != "" {
		agent.SetPref("CloudhookURL", r.CloudhookURL)
	}
	if r.RemoteUIURL != "" {
		agent.SetPref("RemoteUIURL", r.RemoteUIURL)
	}
	if r.Secret != "" {
		agent.SetPref("Secret", r.Secret)
	}
	if r.WebhookID != "" {
		agent.SetPref("WebhookID", r.WebhookID)
	}
	agent.SetPref("Registered", true)

	// ! https://github.com/fyne-io/fyne/issues/3170
	time.Sleep(110 * time.Millisecond)
}

// registerWithUI handles a registration flow via a graphical interface
func (agent *Agent) registerWithUI(ctx context.Context, registration *hass.RegistrationDetails) (*hass.RegistrationResponse, error) {
	done := make(chan struct{})
	agent.registrationWindow(ctx, registration, done)
	<-done
	if !registration.Validate() {
		return nil, errors.New("registration details not complete")
	}
	return hass.RegisterWithHass(registration)
}

// registerWithoutUI handles a registration flow without any graphical interface
// (using values provided via the command-line).
func (agent *Agent) registerWithoutUI(ctx context.Context, registration *hass.RegistrationDetails) (*hass.RegistrationResponse, error) {
	if !registration.Validate() {
		log.Debug().Msg("Registration details not complete.")
		return nil, errors.New("registration details not complete")
	}
	return hass.RegisterWithHass(registration)
}

func (agent *Agent) registrationProcess(ctx context.Context, server, token string, headless bool, done chan struct{}) {
	registration := agent.newRegistration(ctx, server, token)
	var registrationResponse *hass.RegistrationResponse
	var err error
	if headless {
		registrationResponse, err = agent.registerWithoutUI(ctx, registration)
	} else {
		registrationResponse, err = agent.registerWithUI(ctx, registration)
	}
	if err != nil {
		log.Fatal().Err(err).Msg("Could not register device with Home Assistant.")
	}

	agent.saveRegistration(registrationResponse, registration)
	close(done)
}

// findServers is a helper function to generate a list of Home Assistant servers
// via local network auto-discovery.
func findServers(ctx context.Context) binding.StringList {

	serverList := binding.NewStringList()

	// add http://localhost:8123 to the list of servers as a fall-back/default
	// option
	serverList.Append("localhost:8123")

	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		log.Debug().Err(err).Msg("Failed to initialize resolver.")
	} else {
		entries := make(chan *zeroconf.ServiceEntry)
		go func(results <-chan *zeroconf.ServiceEntry) {
			for entry := range results {
				server := entry.AddrIPv4[0].String() + ":" + fmt.Sprint(entry.Port)
				serverList.Append(server)
				log.Debug().Caller().
					Msg("Found a HA instance via mDNS")
			}
		}(entries)

		log.Info().Msg("Looking for Home Assistant instances on the network...")
		searchCtx, searchCancel := context.WithTimeout(ctx, time.Second*5)
		defer searchCancel()
		err = resolver.Browse(searchCtx, "_home-assistant._tcp", "local.", entries)
		if err != nil {
			log.Debug().Err(err).Msg("Failed to browse")
		}

		<-searchCtx.Done()
	}
	return serverList
}

// newHostPort is a custom fyne validator that will validate a string is a
// valid hostname:port combination
func newHostPort() fyne.StringValidator {
	v := validate.New()
	return func(text string) error {
		if err := v.Var(text, "hostname_port"); err != nil {
			return errors.New("you need to specify a valid hostname:port combination")
		}
		return nil
	}
}
