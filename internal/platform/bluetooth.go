package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// Bluetooth asks BlueZ, over the system D-Bus, which audio device is
// connected, for the status bar. The firmware's own settings pair and connect
// devices; Boombrick only reads. It is called from the status reader's
// goroutine, never the UI thread.
type Bluetooth struct {
	sys   string // the sysfs tree the adapters are listed in
	mu    sync.Mutex
	conn  *dbus.Conn
	retry time.Time // when to ask again after BlueZ or the bus failed
}

// retryBus is how long a missing bus or BlueZ is left before asking again.
const retryBus = time.Minute

// NewBluetooth returns a Bluetooth that asks BlueZ only while the sysfs tree
// at sys, normally "/sys", lists an adapter.
func NewBluetooth(sys string) *Bluetooth { return &Bluetooth{sys: sys} }

// Connected is the name of the connected Bluetooth audio device, or "" when
// none is connected or BlueZ isn't there to ask.
func (b *Bluetooth) Connected() string {
	if !hasAdapter(b.sys) {
		return "" // nothing to connect with, as when Bluetooth is off
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if time.Now().Before(b.retry) {
		return ""
	}
	if b.conn == nil {
		conn, err := dbus.ConnectSystemBus()
		if err != nil {
			b.retry = time.Now().Add(retryBus)
			return ""
		}
		b.conn = conn
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var objects map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	err := b.conn.Object("org.bluez", "/").CallWithContext(ctx, "org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&objects)
	if err != nil {
		// An error the bus sent back, as when BlueZ isn't running, leaves the
		// connection fine to ask again; anything else, as the bus going away,
		// doesn't.
		if !replied(err) {
			b.conn.Close()
			b.conn = nil
		}
		b.retry = time.Now().Add(retryBus)
		return ""
	}
	return connectedAudio(objects)
}

// Close lets go of the bus.
func (b *Bluetooth) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.conn != nil {
		b.conn.Close()
		b.conn = nil
	}
}

// replied reports whether err is an error reply from the bus or a service on
// it.
func replied(err error) bool {
	var e dbus.Error
	return errors.As(err, &e)
}

// hasAdapter reports whether the sysfs tree at sys lists a Bluetooth adapter.
func hasAdapter(sys string) bool {
	entries, _ := os.ReadDir(filepath.Join(sys, "class", "bluetooth"))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "hci") {
			return true
		}
	}
	return false
}

// connectedAudio picks the connected audio device from BlueZ's objects:
// headphones, a headset or a speaker, by the icon BlueZ gives it, in path
// order. Its alias is the name the user gave it, or its own.
func connectedAudio(objects map[dbus.ObjectPath]map[string]map[string]dbus.Variant) string {
	paths := make([]dbus.ObjectPath, 0, len(objects))
	for p := range objects {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	for _, p := range paths {
		dev, ok := objects[p]["org.bluez.Device1"]
		if !ok {
			continue
		}
		connected, _ := dev["Connected"].Value().(bool)
		icon, _ := dev["Icon"].Value().(string)
		if !connected || !strings.HasPrefix(icon, "audio-") {
			continue
		}
		for _, key := range []string{"Alias", "Name"} {
			if name, _ := dev[key].Value().(string); name != "" {
				return name
			}
		}
	}
	return ""
}
