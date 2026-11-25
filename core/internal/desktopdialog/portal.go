package desktopdialog

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const portalDesktopBusName = "org.freedesktop.portal.Desktop"
const portalRequestInterface = "org.freedesktop.portal.Request"
const portalDesktopObjectPath = "/org/freedesktop/portal/desktop"

// portalFileChooserAvailable reports whether a session bus with the
// FileChooser portal is reachable.
func portalFileChooserAvailable(ctx context.Context) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	conn, err := dialDBusSession()
	if err != nil {
		return false
	}
	defer conn.Close()
	stopWatch := conn.watchContext(probeCtx)
	defer stopWatch()
	reply, err := conn.callDBusMethod("org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "NameHasOwner", "s", encodeDBusStringBody(portalDesktopBusName))
	if err != nil {
		return false
	}
	d := dbusDecoder{data: reply}
	owned, err := d.value("b")
	return err == nil && owned == true
}

func encodeDBusStringBody(s string) []byte {
	var e dbusEncoder
	e.str(s)
	return e.buf
}

func pickFilesPortal(ctx context.Context, multiple bool) ([]string, error) {
	return runPortalFileChooser(ctx, "选择文件", multiple, false)
}

func pickFolderPortal(ctx context.Context) (string, error) {
	paths, err := runPortalFileChooser(ctx, "选择文件夹", false, true)
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

// runPortalFileChooser calls org.freedesktop.portal.FileChooser.OpenFile and
// waits for the Request.Response signal on the same connection. The portal
// closes the request when the calling connection disconnects, so this
// connection must stay alive for the whole user interaction.
func runPortalFileChooser(ctx context.Context, title string, multiple, directory bool) ([]string, error) {
	conn, err := dialDBusSession()
	if err != nil {
		return nil, fmt.Errorf("connect D-Bus session for file picker: %w", err)
	}
	defer conn.Close()
	stopWatch := conn.watchContext(ctx)
	defer stopWatch()

	matchRule := "type='signal',sender='" + portalDesktopBusName + "',interface='" + portalRequestInterface + "',member='Response'"
	if _, err := conn.callDBusMethod("org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "AddMatch", "s", encodeDBusStringBody(matchRule)); err != nil {
		return nil, fmt.Errorf("subscribe to portal responses: %w", err)
	}

	token, err := newPortalHandleToken()
	if err != nil {
		return nil, err
	}
	requestPath := portalRequestPathForToken(conn.uniqueName, token)

	serial := conn.nextSerial()
	body := encodePortalOpenFileBody(title, multiple, directory, token)
	if err := conn.writeDBusCall(serial, portalDesktopBusName, portalDesktopObjectPath, "org.freedesktop.portal.FileChooser", "OpenFile", "ssa{sv}", body); err != nil {
		return nil, fmt.Errorf("portal FileChooser.OpenFile call failed: %w", err)
	}

	for {
		msg, err := conn.readDBusMessage()
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				conn.sendPortalRequestClose(requestPath)
				return nil, ctxErr
			}
			return nil, fmt.Errorf("read portal response: %w", err)
		}
		if msg.msgType == dbusTypeError && msg.replySerial == serial {
			return nil, fmt.Errorf("portal FileChooser.OpenFile failed: %s", msg.errName)
		}
		if msg.msgType == dbusTypeSignal && msg.iface == portalRequestInterface && msg.member == "Response" && msg.path == requestPath {
			return decodePortalResponseBody(msg.body)
		}
	}
}

// portalRequestPathForToken predicts the Request object path the portal will
// export: /org/freedesktop/portal/desktop/request/<sender>/<handle_token>,
// where the sender segment is our unique name without ':' and with '_' for '.'.
func portalRequestPathForToken(uniqueName, token string) string {
	sender := strings.ReplaceAll(strings.TrimPrefix(uniqueName, ":"), ".", "_")
	return "/org/freedesktop/portal/desktop/request/" + sender + "/" + token
}

func encodePortalOpenFileBody(title string, multiple, directory bool, token string) []byte {
	var e dbusEncoder
	e.str("")
	e.str(title)
	e.value("a{sv}", []dbusDictEntry{
		{key: "multiple", sig: "b", value: multiple},
		{key: "directory", sig: "b", value: directory},
		{key: "handle_token", sig: "s", value: token},
	})
	return e.buf
}

func newPortalHandleToken() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "nshsend" + hex.EncodeToString(buf), nil
}

// sendPortalRequestClose best-effort dismisses a pending request on context
// cancellation so the dialog does not outlive the caller.
func (c *dbusConn) sendPortalRequestClose(requestPath string) {
	_ = c.writeDBusCall(c.nextSerial(), portalDesktopBusName, requestPath, portalRequestInterface, "Close", "", nil)
}

// decodePortalResponseBody decodes the (u, a{sv}) Response payload:
// code 0 = success with file URIs, 1 = user cancelled, 2 = other error.
func decodePortalResponseBody(body []byte) ([]string, error) {
	d := dbusDecoder{data: body}
	codeVal, err := d.value("u")
	if err != nil {
		return nil, fmt.Errorf("decode portal response code: %w", err)
	}
	code, _ := codeVal.(uint32)
	switch code {
	case 0:
	case 1:
		return nil, nil
	default:
		return nil, fmt.Errorf("portal file chooser failed with response code %d", code)
	}
	optsVal, err := d.value("a{sv}")
	if err != nil {
		return nil, fmt.Errorf("decode portal response options: %w", err)
	}
	opts, _ := optsVal.(map[string]any)
	urisVal, ok := opts["uris"]
	if !ok {
		return nil, nil
	}
	uris, ok := urisVal.([]any)
	if !ok {
		return nil, errors.New("portal response uris is not a string array")
	}
	var paths []string
	for _, uriVal := range uris {
		uri, ok := uriVal.(string)
		if !ok {
			return nil, errors.New("portal response uri is not a string")
		}
		path, pathErr := portalURIToPath(uri)
		if pathErr != nil {
			return nil, pathErr
		}
		if path != "" {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

func portalURIToPath(uri string) (string, error) {
	if !strings.HasPrefix(uri, "file://") {
		return "", nil
	}
	path, err := url.PathUnescape(strings.TrimPrefix(uri, "file://"))
	if err != nil {
		return "", fmt.Errorf("decode portal file URI %q: %w", uri, err)
	}
	return path, nil
}