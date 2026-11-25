package desktopdialog

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// Minimal D-Bus wire protocol client (little-endian, protocol version 1),
// enough to call methods on the session bus and receive directed signals.
// The portal closes a FileChooser request as soon as the calling connection
// disconnects, so the caller must keep one connection alive across the call
// and the user-driven Response signal — short-lived CLI helpers cannot do that.

const (
	dbusTypeMethodCall   byte = 1
	dbusTypeMethodReturn byte = 2
	dbusTypeError        byte = 3
	dbusTypeSignal       byte = 4

	dbusFieldPath        byte = 1
	dbusFieldInterface   byte = 2
	dbusFieldMember      byte = 3
	dbusFieldErrorName   byte = 4
	dbusFieldReplySerial byte = 5
	dbusFieldDestination byte = 6
	dbusFieldSender      byte = 7
	dbusFieldSignature   byte = 8
)

// dbusMessage is one decoded D-Bus message.
type dbusMessage struct {
	msgType     byte
	serial      uint32
	replySerial uint32
	path        string
	iface       string
	member      string
	sender      string
	errName     string
	signature   string
	body        []byte
}

// dbusConn is a single authenticated connection to the session bus.
// All reads and writes happen from one goroutine.
type dbusConn struct {
	conn       net.Conn
	reader     *bufio.Reader
	serial     uint32
	uniqueName string
}

// parseDBusSessionAddress turns a DBUS_SESSION_BUS_ADDRESS value into a
// net.Dial network/address pair. Only unix transports are supported.
func parseDBusSessionAddress(addr string) (network, address string, err error) {
	for _, candidate := range strings.Split(addr, ";") {
		if !strings.HasPrefix(candidate, "unix:") {
			continue
		}
		for _, kv := range strings.Split(strings.TrimPrefix(candidate, "unix:"), ",") {
			key, value, ok := strings.Cut(kv, "=")
			if !ok {
				continue
			}
			switch key {
			case "path":
				return "unix", value, nil
			case "abstract":
				return "unix", "@" + value, nil
			}
		}
	}
	return "", "", fmt.Errorf("no usable unix transport in D-Bus session address %q", addr)
}

// dbusSessionAddress resolves the session bus address, falling back to the
// standard systemd location when the environment is sanitized (NSH's
// process.spawn strips DBUS_SESSION_BUS_ADDRESS and XDG_RUNTIME_DIR).
func dbusSessionAddress() string {
	if addr := os.Getenv("DBUS_SESSION_BUS_ADDRESS"); addr != "" {
		return addr
	}
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	return "unix:path=" + runtimeDir + "/bus"
}

// dialDBusSession connects to the session bus, authenticates with SASL
// EXTERNAL, and resolves our unique name via Hello.
func dialDBusSession() (*dbusConn, error) {
	network, address, err := parseDBusSessionAddress(dbusSessionAddress())
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout(network, address, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("dial D-Bus session socket: %w", err)
	}
	c := &dbusConn{conn: conn, reader: bufio.NewReader(conn)}
	if err := c.authenticateDBusExternal(); err != nil {
		conn.Close()
		return nil, err
	}
	reply, err := c.callDBusMethod("org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "Hello", "", nil)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("D-Bus Hello: %w", err)
	}
	d := dbusDecoder{data: reply}
	name, err := d.str()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("decode D-Bus Hello reply: %w", err)
	}
	c.uniqueName = name
	return c, nil
}

func (c *dbusConn) authenticateDBusExternal() error {
	uidHex := hex.EncodeToString([]byte(strconv.Itoa(os.Getuid())))
	if _, err := c.conn.Write([]byte{0}); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(c.conn, "AUTH EXTERNAL %s\r\n", uidHex); err != nil {
		return err
	}
	line, err := c.reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read SASL reply: %w", err)
	}
	if !strings.HasPrefix(line, "OK ") {
		return fmt.Errorf("SASL EXTERNAL rejected: %q", strings.TrimSpace(line))
	}
	if _, err := c.conn.Write([]byte("BEGIN\r\n")); err != nil {
		return err
	}
	return nil
}

// watchContext closes the connection when ctx is cancelled, unblocking reads.
func (c *dbusConn) watchContext(ctx context.Context) func() bool {
	return context.AfterFunc(ctx, func() { c.conn.Close() })
}

func (c *dbusConn) Close() error { return c.conn.Close() }

func (c *dbusConn) nextSerial() uint32 {
	c.serial++
	return c.serial
}

// callDBusMethod sends a method call and waits for its reply, skipping signals.
func (c *dbusConn) callDBusMethod(dest, path, iface, member, bodySig string, body []byte) ([]byte, error) {
	serial := c.nextSerial()
	if err := c.writeDBusCall(serial, dest, path, iface, member, bodySig, body); err != nil {
		return nil, err
	}
	for {
		msg, err := c.readDBusMessage()
		if err != nil {
			return nil, err
		}
		if msg.replySerial != serial {
			continue
		}
		switch msg.msgType {
		case dbusTypeMethodReturn:
			return msg.body, nil
		case dbusTypeError:
			return nil, fmt.Errorf("D-Bus error %s: %s", msg.errName, firstDBusStringArg(msg.body))
		}
	}
}

func (c *dbusConn) writeDBusCall(serial uint32, dest, path, iface, member, bodySig string, body []byte) error {
	fields := []dbusHeaderField{
		{code: dbusFieldPath, sig: "o", value: path},
		{code: dbusFieldInterface, sig: "s", value: iface},
		{code: dbusFieldMember, sig: "s", value: member},
		{code: dbusFieldDestination, sig: "s", value: dest},
	}
	if bodySig != "" {
		fields = append(fields, dbusHeaderField{code: dbusFieldSignature, sig: "g", value: bodySig})
	}
	_, err := c.conn.Write(encodeDBusMessage(dbusTypeMethodCall, serial, fields, body))
	return err
}

// readDBusMessage reads one framed message from the connection.
func (c *dbusConn) readDBusMessage() (*dbusMessage, error) {
	var fixed [16]byte
	if _, err := io.ReadFull(c.reader, fixed[:]); err != nil {
		return nil, err
	}
	if fixed[0] != 'l' {
		return nil, errors.New("unsupported D-Bus endianness")
	}
	bodyLen := binary.LittleEndian.Uint32(fixed[4:8])
	fieldsLen := binary.LittleEndian.Uint32(fixed[12:16])
	headerEnd := 16 + int(fieldsLen)
	bodyStart := (headerEnd + 7) &^ 7
	rest := make([]byte, bodyStart-16+int(bodyLen))
	if _, err := io.ReadFull(c.reader, rest); err != nil {
		return nil, err
	}
	full := append(fixed[:], rest...)
	return decodeDBusMessage(full)
}

func firstDBusStringArg(body []byte) string {
	d := dbusDecoder{data: body}
	s, err := d.str()
	if err != nil {
		return ""
	}
	return s
}

// dbusHeaderField is one header field: code plus a variant value.
type dbusHeaderField struct {
	code  byte
	sig   string
	value any
}

func encodeDBusMessage(msgType byte, serial uint32, fields []dbusHeaderField, body []byte) []byte {
	var f dbusEncoder
	for _, field := range fields {
		f.align(8)
		f.byte(field.code)
		f.variant(field.sig, field.value)
	}
	var m dbusEncoder
	m.byte('l')
	m.byte(msgType)
	m.byte(0)
	m.byte(1)
	m.uint32(uint32(len(body)))
	m.uint32(serial)
	m.uint32(uint32(len(f.buf)))
	m.buf = append(m.buf, f.buf...)
	m.align(8)
	m.buf = append(m.buf, body...)
	return m.buf
}

func decodeDBusMessage(data []byte) (*dbusMessage, error) {
	if len(data) < 16 {
		return nil, io.ErrUnexpectedEOF
	}
	msg := &dbusMessage{msgType: data[1], serial: binary.LittleEndian.Uint32(data[8:12])}
	fieldsLen := int(binary.LittleEndian.Uint32(data[12:16]))
	bodyLen := int(binary.LittleEndian.Uint32(data[4:8]))
	headerEnd := 16 + fieldsLen
	bodyStart := (headerEnd + 7) &^ 7
	if bodyStart+bodyLen > len(data) {
		return nil, io.ErrUnexpectedEOF
	}
	d := dbusDecoder{data: data[:headerEnd]}
	d.pos = 16
	for d.pos < headerEnd {
		if err := d.align(8); err != nil {
			return nil, err
		}
		code, err := d.byte()
		if err != nil {
			return nil, err
		}
		sig, err := d.signature()
		if err != nil {
			return nil, err
		}
		value, err := d.value(sig)
		if err != nil {
			return nil, err
		}
		switch code {
		case dbusFieldPath:
			msg.path, _ = value.(string)
		case dbusFieldInterface:
			msg.iface, _ = value.(string)
		case dbusFieldMember:
			msg.member, _ = value.(string)
		case dbusFieldErrorName:
			msg.errName, _ = value.(string)
		case dbusFieldReplySerial:
			msg.replySerial, _ = value.(uint32)
		case dbusFieldSender:
			msg.sender, _ = value.(string)
		case dbusFieldSignature:
			msg.signature, _ = value.(string)
		}
	}
	msg.body = data[bodyStart : bodyStart+bodyLen]
	return msg, nil
}

// dbusEncoder appends little-endian D-Bus marshaled data with alignment.
type dbusEncoder struct{ buf []byte }

func (e *dbusEncoder) align(n int) {
	for len(e.buf)%n != 0 {
		e.buf = append(e.buf, 0)
	}
}

func (e *dbusEncoder) byte(b byte) { e.buf = append(e.buf, b) }

func (e *dbusEncoder) uint32(v uint32) {
	e.align(4)
	e.buf = binary.LittleEndian.AppendUint32(e.buf, v)
}

func (e *dbusEncoder) str(s string) {
	e.uint32(uint32(len(s)))
	e.buf = append(e.buf, s...)
	e.buf = append(e.buf, 0)
}

func (e *dbusEncoder) signature(s string) {
	e.byte(byte(len(s)))
	e.buf = append(e.buf, s...)
	e.buf = append(e.buf, 0)
}

func (e *dbusEncoder) variant(sig string, value any) {
	e.signature(sig)
	e.value(sig, value)
}

// array writes an array with a patched length prefix; elemAlign is the
// alignment of the element type (8 for dict entries and structs).
func (e *dbusEncoder) array(elemAlign int, writeElems func()) {
	e.align(4)
	lenPos := len(e.buf)
	e.uint32(0)
	e.align(elemAlign)
	start := len(e.buf)
	writeElems()
	binary.LittleEndian.PutUint32(e.buf[lenPos:], uint32(len(e.buf)-start))
}

func (e *dbusEncoder) value(sig string, value any) {
	switch sig {
	case "s", "o":
		e.str(value.(string))
	case "g":
		e.signature(value.(string))
	case "u":
		e.uint32(value.(uint32))
	case "b":
		if value.(bool) {
			e.uint32(1)
		} else {
			e.uint32(0)
		}
	case "as":
		e.array(4, func() {
			for _, s := range value.([]string) {
				e.str(s)
			}
		})
	case "a{sv}":
		e.array(8, func() {
			for _, entry := range value.([]dbusDictEntry) {
				e.align(8)
				e.str(entry.key)
				e.variant(entry.sig, entry.value)
			}
		})
	default:
		panic("dbusEncoder: unsupported signature " + sig)
	}
}

// dbusDictEntry is one string-keyed variant dict entry for a{sv} encoding.
type dbusDictEntry struct {
	key   string
	sig   string
	value any
}

// dbusDecoder reads little-endian D-Bus data with alignment and bounds checks.
type dbusDecoder struct {
	data []byte
	pos  int
}

func (d *dbusDecoder) align(n int) error {
	for d.pos%n != 0 {
		if d.pos >= len(d.data) {
			return io.ErrUnexpectedEOF
		}
		d.pos++
	}
	return nil
}

func (d *dbusDecoder) byte() (byte, error) {
	if d.pos >= len(d.data) {
		return 0, io.ErrUnexpectedEOF
	}
	b := d.data[d.pos]
	d.pos++
	return b, nil
}

func (d *dbusDecoder) uint32() (uint32, error) {
	if err := d.align(4); err != nil {
		return 0, err
	}
	if d.pos+4 > len(d.data) {
		return 0, io.ErrUnexpectedEOF
	}
	v := binary.LittleEndian.Uint32(d.data[d.pos:])
	d.pos += 4
	return v, nil
}

func (d *dbusDecoder) str() (string, error) {
	n, err := d.uint32()
	if err != nil {
		return "", err
	}
	if d.pos+int(n)+1 > len(d.data) {
		return "", io.ErrUnexpectedEOF
	}
	s := string(d.data[d.pos : d.pos+int(n)])
	d.pos += int(n) + 1
	return s, nil
}

func (d *dbusDecoder) signature() (string, error) {
	n, err := d.byte()
	if err != nil {
		return "", err
	}
	if d.pos+int(n)+1 > len(d.data) {
		return "", io.ErrUnexpectedEOF
	}
	s := string(d.data[d.pos : d.pos+int(n)])
	d.pos += int(n) + 1
	return s, nil
}

func dbusTypeAlignment(first byte) int {
	switch first {
	case 'y', 'g', 'v':
		return 1
	case 'n', 'q':
		return 2
	case 'x', 't', 'd', '(', '{':
		return 8
	default:
		return 4
	}
}

// value decodes one value of the given signature. Arrays yield []any,
// a{sv} yields map[string]any, variants unwrap to their inner value.
func (d *dbusDecoder) value(sig string) (any, error) {
	if sig == "" {
		return nil, errors.New("empty D-Bus signature")
	}
	switch sig[0] {
	case 'y':
		return d.byte()
	case 'b':
		v, err := d.uint32()
		return v != 0, err
	case 'u':
		return d.uint32()
	case 'i':
		v, err := d.uint32()
		return int32(v), err
	case 'n', 'q':
		if err := d.align(2); err != nil {
			return nil, err
		}
		if d.pos+2 > len(d.data) {
			return nil, io.ErrUnexpectedEOF
		}
		v := binary.LittleEndian.Uint16(d.data[d.pos:])
		d.pos += 2
		if sig[0] == 'n' {
			return int16(v), nil
		}
		return v, nil
	case 'x', 't', 'd':
		if err := d.align(8); err != nil {
			return nil, err
		}
		if d.pos+8 > len(d.data) {
			return nil, io.ErrUnexpectedEOF
		}
		v := binary.LittleEndian.Uint64(d.data[d.pos:])
		d.pos += 8
		if sig[0] == 'x' {
			return int64(v), nil
		}
		return v, nil
	case 's', 'o':
		return d.str()
	case 'g':
		return d.signature()
	case 'v':
		inner, err := d.signature()
		if err != nil {
			return nil, err
		}
		return d.value(inner)
	case 'a':
		elemSig := sig[1:]
		n, err := d.uint32()
		if err != nil {
			return nil, err
		}
		if err := d.align(dbusTypeAlignment(elemSig[0])); err != nil {
			return nil, err
		}
		end := d.pos + int(n)
		if end > len(d.data) {
			return nil, io.ErrUnexpectedEOF
		}
		if elemSig[0] == '{' {
			inner := elemSig[1 : len(elemSig)-1]
			keySig, valSig, err := splitDBusDictEntrySignature(inner)
			if err != nil {
				return nil, err
			}
			out := map[string]any{}
			for d.pos < end {
				if err := d.align(8); err != nil {
					return nil, err
				}
				key, err := d.value(keySig)
				if err != nil {
					return nil, err
				}
				val, err := d.value(valSig)
				if err != nil {
					return nil, err
				}
				keyStr, ok := key.(string)
				if !ok {
					return nil, errors.New("non-string D-Bus dict key")
				}
				out[keyStr] = val
			}
			return out, nil
		}
		var out []any
		for d.pos < end {
			elem, err := d.value(elemSig)
			if err != nil {
				return nil, err
			}
			out = append(out, elem)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported D-Bus signature %q", sig)
	}
}

// splitDBusDictEntrySignature splits "{sv}" inner signatures into key and
// value parts; the key must be a single basic type.
func splitDBusDictEntrySignature(inner string) (keySig, valSig string, err error) {
	if inner == "" {
		return "", "", errors.New("empty dict entry signature")
	}
	return inner[:1], inner[1:], nil
}