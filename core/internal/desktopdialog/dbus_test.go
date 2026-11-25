package desktopdialog

import (
	"fmt"
	"os"
	"reflect"
	"testing"
)

func TestDBusSessionAddressFallback(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/custom/bus")
	if got := dbusSessionAddress(); got != "unix:path=/custom/bus" {
		t.Fatalf("explicit address = %q", got)
	}
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/4242")
	if got := dbusSessionAddress(); got != "unix:path=/run/user/4242/bus" {
		t.Fatalf("runtime dir fallback = %q", got)
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	want := "unix:path=" + fmt.Sprintf("/run/user/%d", os.Getuid()) + "/bus"
	if got := dbusSessionAddress(); got != want {
		t.Fatalf("uid fallback = %q, want %q", got, want)
	}
}

func TestParseDBusSessionAddress(t *testing.T) {
	cases := []struct {
		name        string
		addr        string
		wantNetwork string
		wantAddress string
		wantErr     bool
	}{
		{name: "unix path", addr: "unix:path=/run/user/1000/bus", wantNetwork: "unix", wantAddress: "/run/user/1000/bus"},
		{name: "with guid", addr: "unix:path=/run/user/1000/bus,guid=abc123", wantNetwork: "unix", wantAddress: "/run/user/1000/bus"},
		{name: "abstract socket", addr: "unix:abstract=/tmp/dbus-test", wantNetwork: "unix", wantAddress: "@/tmp/dbus-test"},
		{name: "fallback candidate", addr: "tcp:host=localhost,port=1;unix:path=/run/user/1000/bus", wantNetwork: "unix", wantAddress: "/run/user/1000/bus"},
		{name: "no unix transport", addr: "tcp:host=localhost,port=1", wantErr: true},
		{name: "empty", addr: "", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			network, address, err := parseDBusSessionAddress(tc.addr)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %s %s", network, address)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if network != tc.wantNetwork || address != tc.wantAddress {
				t.Fatalf("got %s %s, want %s %s", network, address, tc.wantNetwork, tc.wantAddress)
			}
		})
	}
}

func TestDBusMethodCallRoundTrip(t *testing.T) {
	body := encodePortalOpenFileBody("选择文件", true, false, "nshsendabc123")
	fields := []dbusHeaderField{
		{code: dbusFieldPath, sig: "o", value: portalDesktopObjectPath},
		{code: dbusFieldInterface, sig: "s", value: "org.freedesktop.portal.FileChooser"},
		{code: dbusFieldMember, sig: "s", value: "OpenFile"},
		{code: dbusFieldDestination, sig: "s", value: portalDesktopBusName},
		{code: dbusFieldSignature, sig: "g", value: "ssa{sv}"},
	}
	raw := encodeDBusMessage(dbusTypeMethodCall, 42, fields, body)
	msg, err := decodeDBusMessage(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if msg.msgType != dbusTypeMethodCall || msg.serial != 42 {
		t.Fatalf("type/serial = %d/%d", msg.msgType, msg.serial)
	}
	if msg.path != portalDesktopObjectPath || msg.iface != "org.freedesktop.portal.FileChooser" || msg.member != "OpenFile" {
		t.Fatalf("header fields = %+v", msg)
	}
	if msg.signature != "ssa{sv}" {
		t.Fatalf("signature = %q", msg.signature)
	}
	d := dbusDecoder{data: msg.body}
	parent, err := d.str()
	if err != nil || parent != "" {
		t.Fatalf("parent_window = %q, err %v", parent, err)
	}
	title, err := d.str()
	if err != nil || title != "选择文件" {
		t.Fatalf("title = %q, err %v", title, err)
	}
	optsVal, err := d.value("a{sv}")
	if err != nil {
		t.Fatalf("options: %v", err)
	}
	opts := optsVal.(map[string]any)
	if opts["multiple"] != true || opts["directory"] != false || opts["handle_token"] != "nshsendabc123" {
		t.Fatalf("options = %#v", opts)
	}
}

func TestDBusSignalRoundTrip(t *testing.T) {
	var body dbusEncoder
	body.uint32(0)
	body.value("a{sv}", []dbusDictEntry{
		{key: "uris", sig: "as", value: []string{"file:///home/das/a.txt", "file:///home/das/我的 文件.txt"}},
	})
	fields := []dbusHeaderField{
		{code: dbusFieldPath, sig: "o", value: "/org/freedesktop/portal/desktop/request/1_1105/nshsendabc123"},
		{code: dbusFieldInterface, sig: "s", value: portalRequestInterface},
		{code: dbusFieldMember, sig: "s", value: "Response"},
		{code: dbusFieldSender, sig: "s", value: ":1.25"},
		{code: dbusFieldSignature, sig: "g", value: "ua{sv}"},
	}
	raw := encodeDBusMessage(dbusTypeSignal, 7, fields, body.buf)
	msg, err := decodeDBusMessage(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if msg.msgType != dbusTypeSignal || msg.member != "Response" || msg.sender != ":1.25" {
		t.Fatalf("msg = %+v", msg)
	}
	paths, err := decodePortalResponseBody(msg.body)
	if err != nil {
		t.Fatalf("response body: %v", err)
	}
	want := []string{"/home/das/a.txt", "/home/das/我的 文件.txt"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
}

func TestDBusErrorReplyRoundTrip(t *testing.T) {
	var body dbusEncoder
	body.str("something went wrong")
	fields := []dbusHeaderField{
		{code: dbusFieldErrorName, sig: "s", value: "org.freedesktop.DBus.Error.InvalidArgs"},
		{code: dbusFieldReplySerial, sig: "u", value: uint32(42)},
		{code: dbusFieldSignature, sig: "g", value: "s"},
	}
	raw := encodeDBusMessage(dbusTypeError, 9, fields, body.buf)
	msg, err := decodeDBusMessage(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if msg.msgType != dbusTypeError || msg.replySerial != 42 || msg.errName != "org.freedesktop.DBus.Error.InvalidArgs" {
		t.Fatalf("msg = %+v", msg)
	}
	if got := firstDBusStringArg(msg.body); got != "something went wrong" {
		t.Fatalf("error body = %q", got)
	}
}

func TestDBusDecoderRejectsTruncated(t *testing.T) {
	var e dbusEncoder
	e.str("hello")
	e.uint32(7)
	full := e.buf
	for cut := 0; cut < 10; cut++ {
		d := dbusDecoder{data: full[:cut]}
		if _, err := d.str(); err == nil {
			t.Fatalf("cut %d: expected error decoding string", cut)
		}
	}
	d := dbusDecoder{data: full}
	if _, err := d.str(); err != nil {
		t.Fatalf("full string: %v", err)
	}
	if _, err := d.uint32(); err != nil {
		t.Fatalf("full uint32: %v", err)
	}
}

func TestDBusEmptyArrayRoundTrip(t *testing.T) {
	var e dbusEncoder
	e.value("a{sv}", []dbusDictEntry{})
	d := dbusDecoder{data: e.buf}
	val, err := d.value("a{sv}")
	if err != nil {
		t.Fatalf("decode empty dict: %v", err)
	}
	if m, ok := val.(map[string]any); !ok || len(m) != 0 {
		t.Fatalf("val = %#v", val)
	}
}