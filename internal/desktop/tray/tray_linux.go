//go:build linux

package tray

import (
	"errors"
	"github.com/godbus/dbus/v5"
)

// Linux uses the StatusNotifierItem protocol over the user's D-Bus session.
// Activate opens the Gio window; SecondaryActivate toggles the proxy. The
// StatusNotifier tooltip mirrors the same runtime/account summary as Windows.
type linuxTray struct {
	conn   *dbus.Conn
	status Status
	cb     Callbacks
}
type sniObject struct{ t *linuxTray }
type iconPixmap struct {
	Width, Height int32
	Data          []byte
}
type toolTip struct {
	IconName   string
	IconPixmap []iconPixmap
	Title      string
	Text       string
}

func start(cb Callbacks) (Tray, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}
	t := &linuxTray{conn: conn, cb: cb}
	obj := &sniObject{t: t}
	path := dbus.ObjectPath("/StatusNotifierItem")
	if err := conn.Export(obj, path, "org.kde.StatusNotifierItem"); err != nil {
		conn.Close()
		return nil, err
	}
	if err := conn.Export(obj, path, "org.freedesktop.DBus.Properties"); err != nil {
		conn.Close()
		return nil, err
	}
	name := "org.kde.StatusNotifierItem.qoder_proxy"
	reply, err := conn.RequestName(name, dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		conn.Close()
		return nil, errors.New("cannot acquire StatusNotifierItem bus name")
	}
	watcher := conn.Object("org.kde.StatusNotifierWatcher", "/StatusNotifierWatcher")
	if call := watcher.Call("org.kde.StatusNotifierWatcher.RegisterStatusNotifierItem", 0, name); call.Err != nil {
		conn.Close()
		return nil, call.Err
	}
	return t, nil
}
func (t *linuxTray) SetStatus(s Status) {
	t.status = s
	_ = t.conn.Emit("/StatusNotifierItem", "org.freedesktop.DBus.Properties.PropertiesChanged", "org.kde.StatusNotifierItem", map[string]dbus.Variant{"Status": dbus.MakeVariant("Active")}, []string{})
}
func (t *linuxTray) Close() error { return t.conn.Close() }
func (o *sniObject) Activate(x, y int32) *dbus.Error {
	if o.t.cb.OpenWindow != nil {
		go o.t.cb.OpenWindow()
	}
	return nil
}
func (o *sniObject) SecondaryActivate(x, y int32) *dbus.Error {
	if o.t.cb.ToggleProxy != nil {
		go o.t.cb.ToggleProxy()
	}
	return nil
}
func (o *sniObject) ContextMenu(x, y int32) *dbus.Error { return nil }
func (o *sniObject) Get(iface, prop string) (dbus.Variant, *dbus.Error) {
	switch prop {
	case "Category":
		return dbus.MakeVariant("ApplicationStatus"), nil
	case "Id":
		return dbus.MakeVariant("qoder-proxy"), nil
	case "Title":
		return dbus.MakeVariant("Qoder Proxy"), nil
	case "ToolTip":
		return dbus.MakeVariant(toolTip{IconName: "network-transmit-receive", Title: "Qoder Proxy", Text: o.t.status.Tooltip}), nil
	case "Status":
		return dbus.MakeVariant("Active"), nil
	case "IconName":
		return dbus.MakeVariant("network-transmit-receive"), nil
	}
	return dbus.Variant{}, dbus.MakeFailedError(errors.New("unknown property"))
}
func (o *sniObject) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	return map[string]dbus.Variant{
		"Category": dbus.MakeVariant("ApplicationStatus"),
		"Id":       dbus.MakeVariant("qoder-proxy"),
		"Title":    dbus.MakeVariant("Qoder Proxy"),
		"ToolTip":  dbus.MakeVariant(toolTip{IconName: "network-transmit-receive", Title: "Qoder Proxy", Text: o.t.status.Tooltip}),
		"Status":   dbus.MakeVariant("Active"),
		"IconName": dbus.MakeVariant("network-transmit-receive"),
	}, nil
}
func (o *sniObject) Set(iface, prop string, value dbus.Variant) *dbus.Error {
	return dbus.MakeFailedError(errors.New("read-only"))
}
