//go:build linux && !android

package filechooser

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

const portalDest = "org.freedesktop.portal.Desktop"
const portalPath = "/org/freedesktop/portal/desktop"

// portalRequest manages a single portal request/response cycle.
type portalRequest struct {
	conn        *dbus.Conn
	requestPath dbus.ObjectPath
	matchRule   string
	signals     chan *dbus.Signal
}

// portalAvailable checks whether the XDG Desktop Portal service is running.
func portalAvailable(conn *dbus.Conn) bool {
	var hasOwner bool
	err := conn.BusObject().Call(
		"org.freedesktop.DBus.NameHasOwner", 0, portalDest,
	).Store(&hasOwner)
	return err == nil && hasOwner
}

// portalHandleTokenPrefix is the prefix used in handle tokens passed to the
// XDG Desktop Portal. It identifies us as the flugo framework in any portal
// telemetry/logs but has no functional effect.
const portalHandleTokenPrefix = "flugo"

// newPortalRequest sets up a handle token, computes the expected request path,
// subscribes to the Response signal, and returns a portalRequest ready for use.
func newPortalRequest(conn *dbus.Conn) (*portalRequest, error) {
	handleToken := fmt.Sprintf("%s_%d", portalHandleTokenPrefix, time.Now().UnixNano())

	names := conn.Names()
	if len(names) == 0 {
		return nil, fmt.Errorf("dbus connection has no names")
	}
	sender := names[0]
	senderPath := strings.ReplaceAll(sender[1:], ".", "_")
	requestPath := dbus.ObjectPath(fmt.Sprintf(
		"/org/freedesktop/portal/desktop/request/%s/%s", senderPath, handleToken,
	))

	matchRule := fmt.Sprintf(
		"type='signal',interface='org.freedesktop.portal.Request',member='Response',path='%s'",
		requestPath,
	)
	if err := conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, matchRule).Err; err != nil {
		return nil, fmt.Errorf("failed to subscribe to portal response: %w", err)
	}

	signals := make(chan *dbus.Signal, 1)
	conn.Signal(signals)

	return &portalRequest{
		conn:        conn,
		requestPath: requestPath,
		matchRule:   matchRule,
		signals:     signals,
	}, nil
}

// handleToken returns just the token portion for use in portal options.
func (r *portalRequest) handleToken() string {
	parts := strings.Split(string(r.requestPath), "/")
	return parts[len(parts)-1]
}

// wait blocks until the portal sends a Response signal or a 60s timeout occurs.
// Returns the response code and results map.
func (r *portalRequest) wait() (uint32, map[string]dbus.Variant, error) {
	timeout := time.After(60 * time.Second)
	for {
		select {
		case signal := <-r.signals:
			if signal == nil {
				continue
			}
			if signal.Path != r.requestPath || signal.Name != "org.freedesktop.portal.Request.Response" {
				continue
			}
			if len(signal.Body) < 2 {
				return 0, nil, fmt.Errorf("incomplete response from portal")
			}
			response, ok := signal.Body[0].(uint32)
			if !ok {
				return 0, nil, fmt.Errorf("unexpected response type from portal")
			}
			results, ok := signal.Body[1].(map[string]dbus.Variant)
			if !ok {
				return response, nil, nil
			}
			return response, results, nil
		case <-timeout:
			return 0, nil, fmt.Errorf("portal request timed out")
		}
	}
}

// cleanup removes the signal subscription and match rule.
func (r *portalRequest) cleanup() {
	r.conn.RemoveSignal(r.signals)
	r.conn.BusObject().Call("org.freedesktop.DBus.RemoveMatch", 0, r.matchRule)
}

// uriToPath converts a file:// URI to a filesystem path.
func uriToPath(uri string) (string, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("failed to parse URI %q: %w", uri, err)
	}
	if parsed.Scheme != "file" {
		return "", fmt.Errorf("unexpected URI scheme %q (expected file://)", parsed.Scheme)
	}
	return parsed.Path, nil
}

// urisToPath converts a slice of file:// URIs to filesystem paths.
func urisToPath(uris []string) ([]string, error) {
	paths := make([]string, len(uris))
	for i, uri := range uris {
		p, err := uriToPath(uri)
		if err != nil {
			return nil, err
		}
		paths[i] = p
	}
	return paths, nil
}
