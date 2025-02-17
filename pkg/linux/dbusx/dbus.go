// Copyright (c) 2024 Joshua Rich <joshua.rich@gmail.com>
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dbusx

import (
	"context"
	"errors"
	"os/user"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/rs/zerolog/log"
)

const (
	SessionBus        dbusType = iota // session
	SystemBus                         // system
	PropChangedSignal = "org.freedesktop.DBus.Properties.PropertiesChanged"
)

type dbusType int

type Bus struct {
	conn    *dbus.Conn
	busType dbusType
	wg      sync.WaitGroup
}

// NewBus sets up DBus connections and channels for receiving signals. It
// creates both a system and session bus connection.
func NewBus(ctx context.Context, t dbusType) *Bus {
	var conn *dbus.Conn
	var err error
	dbusCtx, cancelFunc := context.WithCancel(context.Background())
	switch t {
	case SessionBus:
		conn, err = dbus.ConnectSessionBus(dbus.WithContext(dbusCtx))
	case SystemBus:
		conn, err = dbus.ConnectSystemBus(dbus.WithContext(dbusCtx))
	}
	if err != nil {
		log.Error().Err(err).Msg("Could not connect to bus.")
		cancelFunc()
		return nil
	}
	b := &Bus{
		conn:    conn,
		busType: t,
	}
	go func() {
		defer conn.Close()
		defer cancelFunc()
		<-ctx.Done()
		b.wg.Wait()
	}()
	return b
}

// busRequest contains properties for building different types of DBus requests.
type busRequest struct {
	bus          *Bus
	eventHandler func(*dbus.Signal)
	path         dbus.ObjectPath
	event        string
	dest         string
	match        []dbus.MatchOption
}

func NewBusRequest(ctx context.Context, busType dbusType) *busRequest {
	if bus, ok := getBus(ctx, busType); !ok {
		log.Debug().Msg("No D-Bus connection present in context.")
		return &busRequest{}
	} else {
		return &busRequest{
			bus: bus,
		}
	}
}

func NewBusRequest2(ctx context.Context, busType dbusType) *busRequest {
	if b := NewBus(ctx, busType); b != nil {
		return &busRequest{
			bus: b,
		}
	} else {
		log.Debug().Msg("No D-Bus connection present in context.")
		return &busRequest{}
	}
}

// Path defines the DBus path on which a request will operate.
func (r *busRequest) Path(p dbus.ObjectPath) *busRequest {
	r.path = p
	return r
}

// Match defines DBus routing match rules on which a request will operate.
func (r *busRequest) Match(m []dbus.MatchOption) *busRequest {
	r.match = m
	return r
}

// Event defines an event on which a DBus request should match.
func (r *busRequest) Event(e string) *busRequest {
	r.event = e
	return r
}

// Handler defines a function that will handle a matched DBus signal.
func (r *busRequest) Handler(h func(*dbus.Signal)) *busRequest {
	r.eventHandler = h
	return r
}

// Destination defines the location/interface on a given DBus path for a request
// to operate.
func (r *busRequest) Destination(d string) *busRequest {
	r.dest = d
	return r
}

// GetProp fetches the specified property from DBus with the options specified
// in the builder.
func (r *busRequest) GetProp(prop string) (dbus.Variant, error) {
	if r.bus == nil {
		return dbus.MakeVariant(""), errors.New("no bus connection")
	}
	obj := r.bus.conn.Object(r.dest, r.path)
	res, err := obj.GetProperty(prop)
	if err != nil {
		log.Debug().Err(err).
			Msgf("Unable to retrieve property %s (%s)", prop, r.dest)
		return dbus.MakeVariant(""), err
	}
	return res, nil
}

// SetProp sets the specific property to the specified value.
func (r *busRequest) SetProp(prop string, value dbus.Variant) error {
	if r.bus != nil {
		obj := r.bus.conn.Object(r.dest, r.path)
		return obj.SetProperty(prop, value)
	}
	return errors.New("no bus connection")
}

// GetData fetches DBus data from the given method in the builder.
func (r *busRequest) GetData(method string, args ...any) *dbusData {
	if r.bus == nil {
		log.Error().Msg("No bus connection.")
		return nil
	}
	d := new(dbusData)
	obj := r.bus.conn.Object(r.dest, r.path)
	var err error
	if args != nil {
		err = obj.Call(method, 0, args...).Store(&d.data)
	} else {
		err = obj.Call(method, 0).Store(&d.data)
	}
	if err != nil {
		log.Debug().Err(err).
			Msgf("Unable to execute %s on %s (args: %s)", method, r.dest, args)
	}
	return d
}

// Call executes the given method in the builder and returns the error state.
func (r *busRequest) Call(method string, args ...any) error {
	if r.bus == nil {
		return errors.New("no bus connection")
	}
	obj := r.bus.conn.Object(r.dest, r.path)
	if args != nil {
		return obj.Call(method, 0, args...).Err
	}
	return obj.Call(method, 0).Err
}

func (r *busRequest) AddWatch(ctx context.Context) error {
	if r.bus == nil {
		return errors.New("no bus connection")
	}
	if err := r.bus.conn.AddMatchSignalContext(ctx, r.match...); err != nil {
		return err
	}
	signalCh := make(chan *dbus.Signal)
	r.bus.conn.Signal(signalCh)
	r.bus.wg.Add(1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				r.bus.conn.RemoveSignal(signalCh)
				close(signalCh)
				return
			case signal := <-signalCh:
				r.eventHandler(signal)
			}
		}
	}()
	go func() {
		wg.Wait()
		r.bus.wg.Done()
	}()
	return nil
}

func (r *busRequest) RemoveWatch(ctx context.Context) error {
	if r.bus == nil {
		return errors.New("no bus connection")
	}
	if err := r.bus.conn.RemoveMatchSignalContext(ctx, r.match...); err != nil {
		return err
	}
	log.Trace().
		Str("path", string(r.path)).
		Str("dest", r.dest).
		Str("event", r.event).
		Msgf("Removed D-Bus signal.")
	return nil
}

type dbusData struct {
	data any
}

// AsVariantMap formats DBus data as a map[string]dbus.Variant.
func (d *dbusData) AsVariantMap() map[string]dbus.Variant {
	if d == nil {
		return nil
	}
	wanted := make(map[string]dbus.Variant)
	data, ok := d.data.(map[string]any)
	if !ok {
		log.Debug().Msgf("Could not represent D-Bus data as %T.", wanted)
		return wanted
	}
	for k, v := range data {
		wanted[k] = dbus.MakeVariant(v)
	}
	return wanted
}

// AsStringMap formats DBus data as a map[string]string.
func (d *dbusData) AsStringMap() map[string]string {
	if d == nil {
		return nil
	}
	data, ok := d.data.(map[string]string)
	if !ok {
		log.Debug().Msgf("Could not represent D-Bus data as %T.", data)
		return make(map[string]string)
	}
	return data
}

// AsObjectPathList formats DBus data as a []dbus.ObjectPath.
func (d *dbusData) AsObjectPathList() []dbus.ObjectPath {
	if d == nil {
		return nil
	}
	var data []dbus.ObjectPath
	var ok bool
	data, ok = d.data.([]dbus.ObjectPath)
	if !ok {
		log.Debug().Msgf("Could not represent D-Bus data as %T.", data)
	}
	return data
}

// AsStringList formats DBus data as a []string.
func (d *dbusData) AsStringList() []string {
	if d == nil {
		return nil
	}
	var data []string
	var ok bool
	data, ok = d.data.([]string)
	if !ok {
		log.Debug().Msgf("Could not represent D-Bus data as %T.", data)
	}
	return data
}

// AsObjectPath formats DBus data as a dbus.ObjectPath.
func (d *dbusData) AsObjectPath() dbus.ObjectPath {
	if d == nil {
		return dbus.ObjectPath("")
	}
	var data dbus.ObjectPath
	var ok bool
	data, ok = d.data.(dbus.ObjectPath)
	if !ok {
		log.Debug().Msgf("Could not represent D-Bus data as %T.", data)
	}
	return data
}

// AsRawInterface formats DBus data as a plain interface{}.
func (d *dbusData) AsRawInterface() any {
	if d != nil {
		return d.data
	}
	return nil
}

func GetSessionPath(ctx context.Context) dbus.ObjectPath {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	sessions := NewBusRequest(ctx, SystemBus).
		Path("/org/freedesktop/login1").
		Destination("org.freedesktop.login1").
		GetData("org.freedesktop.login1.Manager.ListSessions").
		AsRawInterface()
	allSessions, ok := sessions.([][]any)
	if !ok {
		return ""
	}
	for _, s := range allSessions {
		if thisUser, ok := s[2].(string); ok && thisUser == u.Username {
			if p, ok := s[4].(dbus.ObjectPath); ok {
				return p
			}
		}
	}
	return ""
}

// VariantToValue converts a dbus.Variant interface{} value into the specified
// Go native type. If the value is nil, then the return value will be the
// default value of the specified type.
func VariantToValue[S any](variant dbus.Variant) S {
	var value S
	err := variant.Store(&value)
	if err != nil {
		log.Debug().Err(err).
			Msgf("Unable to convert dbus variant %v to type %T.", variant, value)
		return value
	}
	return value
}
