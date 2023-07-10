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

	"fyne.io/fyne/v2"
	"github.com/go-playground/validator/v10"
	"github.com/rs/zerolog/log"
	"golang.org/x/mod/semver"
)

const (
	websocketPath = "/api/websocket"
	webHookPath   = "/api/webhook/"
)

func (agent *Agent) AppConfigVersion() string {
	return agent.app.Preferences().String("Version")
}

func (agent *Agent) DeviceDetails() (string, string) {
	return agent.app.Preferences().String("DeviceName"),
		agent.app.Preferences().String("DeviceID")
}

func (agent *Agent) IsRegistered() bool {
	return agent.app.Preferences().BoolWithFallback("Registered", false)
}

func (agent *Agent) SetPref(pref string, value interface{}) {
	if v, ok := value.(string); ok {
		agent.app.Preferences().SetString(pref, v)
		return
	}
	if v, ok := value.(bool); ok {
		agent.app.Preferences().SetBool(pref, v)
		return
	}
}

type agentConfig struct {
	prefs     fyne.Preferences
	validator *validator.Validate
}

func (agent *Agent) LoadConfig() *agentConfig {
	return &agentConfig{
		prefs:     agent.app.Preferences(),
		validator: validator.New(),
	}
}

// agentConfig implements config.Config

func (c *agentConfig) Get(property string) (interface{}, error) {
	switch property {
	case "version":
		return c.prefs.StringWithFallback("Version", Version), nil
	case "websocketURL":
		return c.prefs.String("WebSocketURL"), nil
	case "apiURL":
		return c.prefs.String("ApiURL"), nil
	case "token":
		return c.prefs.String("Token"), nil
	case "webhookID":
		return c.prefs.String("WebhookID"), nil
	case "secret":
		return c.prefs.String("Secret"), nil
	case "host":
		return c.prefs.String("Host"), nil
	case "useTLS":
		return c.prefs.Bool("UseTLS"), nil
	default:
		return nil, fmt.Errorf("unknown config property %s", property)
	}
}

func (c *agentConfig) Set(property string, value interface{}) error {
	if v, ok := value.(string); ok {
		c.prefs.SetString(property, v)
		return nil
	}
	if v, ok := value.(bool); ok {
		c.prefs.SetBool(property, v)
		return nil
	}
	return fmt.Errorf("could not set property %s with value %v", property, value)
}

func (c *agentConfig) Validate() error {
	var value interface{}
	var err error

	value, _ = c.Get("apiURL")
	if c.validator.Var(value, "required,url") != nil {
		return errors.New("apiURL does not match either a URL, hostname or hostname:port")
	}

	value, _ = c.Get("websocketURL")
	if c.validator.Var(value, "required,url") != nil {
		return errors.New("websocketURL does not match either a URL, hostname or hostname:port")
	}

	value, _ = c.Get("token")
	if err = c.validator.Var(value, "required,ascii"); err != nil {
		return errors.New("invalid long-lived token format")
	}

	value, _ = c.Get("webhookID")
	if err = c.validator.Var(value, "required,ascii"); err != nil {
		return errors.New("invalid webhookID format")
	}

	return nil
}

func (c *agentConfig) Refresh(ctx context.Context) error {
	log.Debug().Caller().
		Msg("Agent config does not support refresh.")
	return nil
}

func (c *agentConfig) Upgrade() error {
	configVersion, err := c.Get("version")
	if err != nil {
		return err
	}
	versionString, ok := configVersion.(string)
	if !ok {
		return errors.New("config version is not a valid value")
	}
	switch {
	// * Upgrade host to include scheme for versions < v.1.4.0
	case semver.Compare(versionString, "v1.4.0") < 0:
		log.Debug().Msg("Performing config upgrades for < v1.4.0")
		hostValue, err := c.Get("host")
		if err != nil {
			return err
		}
		hostString, ok := hostValue.(string)
		if !ok {
			return errors.New("upgrade < v.1.4.0: invalid host value")
		}
		tlsValue, err := c.Get("useTLS")
		if err != nil {
			return err
		}
		if useTLS, ok := tlsValue.(bool); !ok {
			hostString = "http://" + hostString
		} else {
			switch useTLS {
			case true:
				hostString = "https://" + hostString
			case false:
				hostString = "http://" + hostString
			}
		}
		if err := c.Set("Host", hostString); err != nil {
			return fmt.Errorf("upgrade < v.1.4.0: could not update host: %v", err)
		}
		fallthrough
	// * Add ApiURL and WebSocketURL config options for versions < v1.4.3
	case semver.Compare(versionString, "v1.4.3") < 0:
		log.Debug().Msg("Performing config upgrades for < v1.4.3")
		c.generateAPIURL()
		c.generateWebsocketURL()
	}

	if err := c.Set("Version", Version); err != nil {
		log.Debug().Err(err).
			Msg("Unable to set new config version.")
	}

	// ! https://github.com/fyne-io/fyne/issues/3170
	time.Sleep(110 * time.Millisecond)

	return nil
}

func (c *agentConfig) generateWebsocketURL() {
	// TODO: look into websocket http upgrade method
	host := c.prefs.String("Host")
	url, _ := url.Parse(host)
	switch url.Scheme {
	case "https":
		url.Scheme = "wss"
	case "http":
		fallthrough
	default:
		url.Scheme = "ws"
	}
	url = url.JoinPath(websocketPath)

	if err := c.Set("WebSocketURL", url.String()); err != nil {
		log.Error().Err(err).
			Msg("Unable to generate web socket URL.")
	}
}

func (c *agentConfig) generateAPIURL() {
	cloudhookURL := c.prefs.String("CloudhookURL")
	remoteUIURL := c.prefs.String("RemoteUIURL")
	webhookID := c.prefs.String("WebhookID")
	host := c.prefs.String("Host")
	var apiURL string
	switch {
	case cloudhookURL != "":
		apiURL = cloudhookURL
	case remoteUIURL != "" && webhookID != "":
		apiURL = remoteUIURL + webHookPath + webhookID
	case webhookID != "" && host != "":
		url, _ := url.Parse(host)
		url = url.JoinPath(webHookPath, webhookID)
		apiURL = url.String()
	default:
		apiURL = ""
	}
	if err := c.Set("ApiURL", apiURL); err != nil {
		log.Error().Err(err).
			Msg("Unable to generate API URL.")
	}
}
