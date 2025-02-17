// Copyright (c) 2024 Joshua Rich <joshua.rich@gmail.com>
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package fyneui

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/cmd/fyne_settings/settings"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/data/validation"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"github.com/go-playground/validator/v10"
	"github.com/rs/zerolog/log"

	"github.com/joshuar/go-hass-agent/internal/agent/ui"
	"github.com/joshuar/go-hass-agent/internal/hass"
	"github.com/joshuar/go-hass-agent/internal/preferences"
	"github.com/joshuar/go-hass-agent/internal/translations"
)

const (
	explainRegistration = `To register the agent, please enter the relevant details for your Home Assistant
server (if not auto-detected) and long-lived access token.`
	restartNote           = `Please restart the agent to use changed preferences.`
	errMsgInvalidURL      = `You need to specify a valid http(s)://host:port.`
	errMsgInvalidURI      = `You need to specify a valid scheme://host:port.`
	errMsgInvalidHostPort = `You need to specify a valid host:port combination.`
)

type fyneUI struct {
	app  fyne.App
	text *translations.Translator
}

func (i *fyneUI) Run(doneCh chan struct{}) {
	if i.app == nil {
		log.Warn().Msg("No supported windowing environment. Will not run UI elements.")
		return
	}
	log.Trace().Msg("Starting Fyne UI loop.")
	go func() {
		<-doneCh
		i.app.Quit()
	}()
	i.app.Run()
}

func (i *fyneUI) DisplayNotification(title, message string) {
	if i.app == nil {
		return
	}
	i.app.SendNotification(&fyne.Notification{
		Title:   title,
		Content: message,
	})
}

// Translate takes the input string and outputs a translated string for the
// language under which the agent is running.
func (i *fyneUI) Translate(text string) string {
	return i.text.Translate(text)
}

func NewFyneUI(id string) *fyneUI {
	i := &fyneUI{
		app:  app.NewWithID(id),
		text: translations.NewTranslator(),
	}
	i.app.SetIcon(&ui.TrayIcon{})
	return i
}

// DisplayTrayIcon displays an icon in the desktop tray with a menu for
// controlling the agent and showing other informational windows.
func (i *fyneUI) DisplayTrayIcon(agent ui.Agent, trk ui.SensorTracker) {
	if desk, ok := i.app.(desktop.App); ok {
		// About menu item.
		menuItemAbout := fyne.NewMenuItem(i.Translate("About"),
			func() {
				i.aboutWindow().Show()
			})
		// Sensors menu item.
		menuItemSensors := fyne.NewMenuItem(i.Translate("Sensors"),
			func() {
				i.sensorsWindow(trk).Show()
			})

		// Settings menu and submenu items.
		settingsMenu := fyne.NewMenuItem(i.Translate("Preferences"), nil)
		settingsMenu.ChildMenu = fyne.NewMenu("",
			fyne.NewMenuItem(i.Translate("App"),
				func() { i.agentSettingsWindow().Show() }),
			fyne.NewMenuItem(i.text.Translate("Fyne"),
				func() { i.fyneSettingsWindow().Show() }),
		)
		// Quit menu item.
		menuItemQuit := fyne.NewMenuItem(i.Translate("Quit"), func() {
			log.Debug().Msg("User requested stop agent.")
			agent.Stop()
		})
		menuItemQuit.IsQuit = true

		menu := fyne.NewMenu("",
			menuItemAbout,
			menuItemSensors,
			settingsMenu,
			menuItemQuit)
		desk.SetSystemTrayMenu(menu)
	}
}

// DisplayRegistrationWindow displays a UI to prompt the user for the details needed to
// complete registration. It will populate with any values that were already
// provided via the command-line.
func (i *fyneUI) DisplayRegistrationWindow(ctx context.Context, server, token *string, done chan struct{}) {
	w := i.app.NewWindow(i.Translate("App Registration"))

	var allFormItems []*widget.FormItem

	allFormItems = append(allFormItems, i.registrationFields(ctx, server, token)...)
	registrationForm := widget.NewForm(allFormItems...)
	registrationForm.OnSubmit = func() {
		w.Close()
		close(done)
	}
	registrationForm.OnCancel = func() {
		log.Warn().Msg("Canceling registration.")
		close(done)
		w.Close()
		ctx.Done()
	}

	w.SetContent(container.New(layout.NewVBoxLayout(),
		widget.NewLabel(i.Translate(explainRegistration)),
		registrationForm,
	))
	log.Debug().Msg("Asking user for registration details.")
	w.Show()
}

// aboutWindow creates a window that will show some interesting information
// about the agent, such as version numbers.
func (i *fyneUI) aboutWindow() fyne.Window {
	haCfg := getHAConfig()
	c := container.NewCenter(container.NewVBox(
		widget.NewLabelWithStyle("Go Hass Agent "+preferences.AppVersion, fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Home Assistant "+haCfg.Version, fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Tracking "+fmt.Sprintf("%d", len(haCfg.Entities))+" Entities", fyne.TextAlignCenter, fyne.TextStyle{Italic: true}),
		widget.NewLabel(""),
		container.NewHBox(
			widget.NewHyperlink("website", parseURL(ui.AppURL)),
			widget.NewLabel("-"),
			widget.NewHyperlink("request feature", parseURL(ui.FeatureRequestURL)),
			widget.NewLabel("-"),
			widget.NewHyperlink("report issue", parseURL(ui.IssueURL)),
		),
	))

	w := i.app.NewWindow(i.Translate("About"))
	w.SetContent(c)
	return w
}

// fyneSettingsWindow creates a window that will show the Fyne settings for
// controlling the look and feel of other windows.
func (i *fyneUI) fyneSettingsWindow() fyne.Window {
	w := i.app.NewWindow(i.Translate("Fyne Preferences"))
	w.SetContent(settings.NewSettings().LoadAppearanceScreen(w))
	return w
}

// agentSettingsWindow creates a window for changing settings related to the
// agent functionality. Most of these settings will be optional.
func (i *fyneUI) agentSettingsWindow() fyne.Window {
	var allFormItems []*widget.FormItem

	prefs, err := preferences.Load()
	if err != nil {
		log.Error().Err(err).Msg("Could not load preferences.")
		return nil
	}

	// MQTT settings
	mqttPrefs := &ui.MQTTPreferences{
		Enabled:  prefs.MQTTEnabled,
		Server:   prefs.MQTTServer,
		User:     prefs.MQTTUser,
		Password: prefs.MQTTPassword,
	}
	allFormItems = append(allFormItems, i.mqttConfigItems(mqttPrefs)...)

	w := i.app.NewWindow(i.Translate("App Preferences"))
	settingsForm := widget.NewForm(allFormItems...)
	settingsForm.OnSubmit = func() {
		err := preferences.Save(
			preferences.MQTTEnabled(mqttPrefs.Enabled),
			preferences.MQTTServer(mqttPrefs.Server),
			preferences.MQTTUser(mqttPrefs.User),
			preferences.MQTTPassword(mqttPrefs.Password),
		)
		if err != nil {
			dialog.ShowError(err, w)
			log.Warn().Err(err).Msg("Could not save MQTT preferences.")
			return
		}
		dialog.ShowInformation("Saved", "MQTT Preferences have been saved.", w)
		log.Info().Msg("Saved MQTT preferences.")
	}
	settingsForm.OnCancel = func() {
		w.Close()
		log.Info().Msg("No MQTT preferences saved.")
	}
	settingsForm.SubmitText = i.Translate("Save")
	w.SetContent(container.New(layout.NewVBoxLayout(),
		widget.NewLabelWithStyle(i.Translate(restartNote), fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		settingsForm,
	))
	return w
}

// sensorsWindow creates a window that displays all of the sensors and their
// values that are currently tracked by the agent. Values are updated
// continuously.
func (i *fyneUI) sensorsWindow(t ui.SensorTracker) fyne.Window {
	sensors := t.SensorList()
	if sensors == nil {
		return nil
	}

	getValue := func(n string) string {
		if v, err := t.Get(n); err == nil {
			var b strings.Builder
			fmt.Fprintf(&b, "%v", v.State())
			if v.Units() != "" {
				fmt.Fprintf(&b, " %s", v.Units())
			}
			return b.String()
		}
		return ""
	}

	sensorsTable := widget.NewTableWithHeaders(
		func() (int, int) {
			return len(sensors), 2
		},
		func() fyne.CanvasObject {
			return widget.NewLabel(longestString(sensors))
		},
		func(i widget.TableCellID, o fyne.CanvasObject) {
			label, ok := o.(*widget.Label)
			if !ok {
				return
			}
			switch i.Col {
			case 0:
				label.SetText(sensors[i.Row])
			case 1:
				label.SetText(getValue(sensors[i.Row]))
			}
		})
	sensorsTable.ShowHeaderColumn = false
	sensorsTable.CreateHeader = func() fyne.CanvasObject {
		return widget.NewLabel("Header")
	}
	sensorsTable.UpdateHeader = func(id widget.TableCellID, template fyne.CanvasObject) {
		label, ok := template.(*widget.Label)
		if !ok {
			return
		}
		if id.Row == -1 && id.Col == 0 {
			label.SetText("Sensor")
		}
		if id.Row == -1 && id.Col == 1 {
			label.SetText("Value")
		}
	}
	// TODO: this is clunky. better way would be use Fyne bindings to sensor values
	doneCh := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Second * 5)
		for {
			select {
			case <-doneCh:
				return
			case <-ticker.C:
				for i, v := range sensors {
					sensorsTable.UpdateCell(widget.TableCellID{
						Row: i,
						Col: 1,
					}, widget.NewLabel(getValue(v)))
				}
				sensorsTable.Refresh()
			}
		}
	}()
	w := i.app.NewWindow(i.Translate("Sensors"))
	w.SetContent(sensorsTable)
	w.Resize(fyne.NewSize(480, 640))
	w.SetOnClosed(func() {
		close(doneCh)
	})
	return w
}

// registrationFields generates a list of form item widgets for selecting a
// server to register the agent against.
func (i *fyneUI) registrationFields(ctx context.Context, server, token *string) []*widget.FormItem {
	allServers := hass.FindServers(ctx)

	if *token == "" {
		*token = "ASecretLongLivedToken"
	}
	tokenEntry := configEntry(token, false)
	tokenEntry.Validator = validation.NewRegexp("[A-Za-z0-9_\\.]+", "Invalid token format")

	if *server == "" {
		*server = allServers[0]
	}
	serverEntry := configEntry(server, false)
	serverEntry.Validator = httpValidator()
	serverEntry.Disable()

	autoServerSelect := widget.NewSelect(allServers, func(s string) {
		serverEntry.SetText(s)
	})

	manualServerEntry := serverEntry
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

	var items []*widget.FormItem

	items = append(items, widget.NewFormItem(i.Translate("Token"), tokenEntry),
		widget.NewFormItem(i.Translate("Auto-discovered Servers"), autoServerSelect),
		widget.NewFormItem(i.Translate("Use Custom Server?"), manualServerSelect),
		widget.NewFormItem(i.Translate("Manual Server Entry"), manualServerEntry))

	return items
}

// mqttConfigItems generates a list of for item widgets for configuring the
// agent to use an MQTT for pub/sub functionality.
func (i *fyneUI) mqttConfigItems(prefs *ui.MQTTPreferences) []*widget.FormItem {
	serverEntry := configEntry(&prefs.Server, false)
	serverEntry.Validator = uriValidator()
	serverEntry.Disable()
	serverFormItem := widget.NewFormItem(i.Translate("MQTT Server"), serverEntry)
	serverFormItem.HintText = ui.MQTTServerHelp

	userEntry := configEntry(&prefs.User, false)
	userEntry.Disable()
	userFormItem := widget.NewFormItem(i.Translate("MQTT User"), userEntry)
	userFormItem.HintText = ui.MQTTUserHelp

	passwordEntry := configEntry(&prefs.Password, true)
	passwordEntry.Disable()
	passwordFormItem := widget.NewFormItem(i.Translate("MQTT Password"), passwordEntry)
	passwordFormItem.HintText = ui.MQTTPasswordHelp

	mqttEnabled := configCheck(&prefs.Enabled, func(b bool) {
		switch b {
		case true:
			serverEntry.Enable()
			userEntry.Enable()
			passwordEntry.Enable()
			prefs.Enabled = true
		case false:
			serverEntry.Disable()
			userEntry.Disable()
			passwordEntry.Disable()
			prefs.Enabled = false
		}
	})

	var items []*widget.FormItem

	items = append(items, widget.NewFormItem(i.Translate("Use MQTT?"), mqttEnabled),
		serverFormItem,
		userFormItem,
		passwordFormItem,
	)

	return items
}

// configEntry creates a form entry widget that is tied to the given config
// value of the given agent. When the value of the entry widget changes, the
// corresponding config value will be updated.
func configEntry(value *string, secret bool) *widget.Entry {
	boundEntry := binding.BindString(value)
	entryWidget := widget.NewEntryWithData(boundEntry)
	entryWidget.Wrapping = fyne.TextWrapWord
	if secret {
		entryWidget.Password = true
	}
	return entryWidget
}

// configCheck creates a form checkbox widget that is tied to the given config
// value of the given agent. When the value of the entry widget changes, the
// corresponding config value will be updated.
func configCheck(value *bool, checkFn func(bool)) *widget.Check {
	entry := widget.NewCheck("", checkFn)
	entry.SetChecked(*value)
	return entry
}

// longestString returns the longest string of a slice of strings. This can be
// used as a placeholder in Fyne containers to ensure there is enough space to
// display any of the strings in the slice.
func longestString(a []string) string {
	var l string
	if len(a) > 0 {
		l = a[0]
		a = a[1:]
	}
	for _, s := range a {
		if len(l) <= len(s) {
			l = s
		}
	}
	return l
}

// httpValidator is a custom fyne validator that will validate a string is a
// valid http/https URL.
func httpValidator() fyne.StringValidator {
	v := validator.New()
	return func(text string) error {
		if v.Var(text, "http_url") != nil {
			return errors.New(errMsgInvalidURL)
		}
		if _, err := url.Parse(text); err != nil {
			return errors.New(errMsgInvalidURL)
		}
		return nil
	}
}

// uriValidator is a custom fyne validator that will validate a string is a
// valid http/https URL.
func uriValidator() fyne.StringValidator {
	v := validator.New()
	return func(text string) error {
		if v.Var(text, "uri") != nil {
			return errors.New(errMsgInvalidURI)
		}
		if _, err := url.Parse(text); err != nil {
			return errors.New(errMsgInvalidURI)
		}
		return nil
	}
}

// hostPortValidator is a custom fyne validator that will validate a string is a
// valid hostname:port combination.
func hostPortValidator(msg string) fyne.StringValidator {
	var errMsg error
	if msg != "" {
		errMsg = errors.New(msg)
	} else {
		errMsg = errors.New(errMsgInvalidHostPort)
	}

	v := validator.New()
	return func(text string) error {
		if v.Var(text, "hostname_port") != nil {
			return errMsg
		}
		// if _, err := url.Parse(text); err != nil {
		// 	return errors.New("string is invalid")
		// }
		return nil
	}
}

// parseURL takes a URL as a string and parses it as a url.URL.
func parseURL(u string) *url.URL {
	dest, err := url.Parse(strings.TrimSpace(u))
	if err != nil {
		log.Warn().Err(err).
			Msgf("Unable parse url %s", u)
	}
	return dest
}

func getHAConfig() *hass.Config {
	prefs, err := preferences.Load()
	if err != nil {
		log.Warn().Err(err).Msg("Could not load preferences.")
	}
	ctx := preferences.EmbedInContext(context.TODO(), prefs)
	haCfg, err := hass.GetConfig(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("Could not fetch HA config.")
	}
	return haCfg
}
