package chromecast

import (
	"encoding/json"
	"log/slog"
	"time"
)

// DashCastAppID is the Cast application id for DashCast, the receiver app
// used by Home Assistant / catt to cast an arbitrary URL to a Chromecast
// device.
const DashCastAppID = "84912283"

const nsDashCast = "urn:x-cast:com.madmod.dashcast"

const requestTimeout = 10 * time.Second

// State describes the current casting status of a device as observed via
// the Cast V2 receiver status.
type State struct {
	// Casting is true if the DashCast app is the currently running/foreground
	// application on the device.
	Casting bool
	// AppDisplayName is the display name of the currently running app, if
	// any (e.g. "Backdrop" when idle, "DashCast" when casting).
	AppDisplayName string
}

// Client controls casting a single URL to a single Chromecast/Nest Hub
// device using the DashCast receiver app.
type Client struct {
	c   *conn
	log *slog.Logger
}

// NewClient creates a client for the device at host:port (port defaults to
// 8009, the standard Cast V2 port, when 0).
func NewClient(host string, port int, log *slog.Logger) *Client {
	if log == nil {
		log = slog.Default()
	}
	return &Client{c: newConn(host, port, log), log: log}
}

// Close disconnects from the device.
func (cl *Client) Close() error {
	return cl.c.close()
}

func (cl *Client) ensureConnected() error {
	if cl.c.isConnected() {
		return nil
	}
	return cl.c.connect()
}

// receiverRequest sends a request on the receiver namespace and waits for
// the matching RECEIVER_STATUS (or LAUNCH_ERROR) reply.
func (cl *Client) receiverRequest(payload map[string]any) (*castMessage, error) {
	requestID := cl.c.nextRequestID()
	payload["requestId"] = requestID

	replyCh := cl.c.waitForRequest(requestID)
	defer cl.c.forgetRequest(requestID)

	if err := cl.c.send(platformID, nsReceiver, payload); err != nil {
		return nil, err
	}

	select {
	case msg := <-replyCh:
		return msg, nil
	case <-time.After(requestTimeout):
		return nil, errTimeout
	}
}

type receiverStatusPayload struct {
	Type   string `json:"type"`
	Status struct {
		Applications []struct {
			AppID       string `json:"appId"`
			DisplayName string `json:"displayName"`
			TransportID string `json:"transportId"`
			SessionID   string `json:"sessionId"`
		} `json:"applications"`
	} `json:"status"`
}

// Status queries the device's current receiver status and reports whether
// DashCast is the active application.
func (cl *Client) Status() (State, error) {
	if err := cl.ensureConnected(); err != nil {
		return State{}, err
	}

	msg, err := cl.receiverRequest(map[string]any{"type": "GET_STATUS"})
	if err != nil {
		return State{}, err
	}

	var status receiverStatusPayload
	if err := json.Unmarshal([]byte(msg.PayloadUTF8), &status); err != nil {
		return State{}, err
	}

	for _, app := range status.Status.Applications {
		if app.AppID == DashCastAppID {
			return State{Casting: true, AppDisplayName: app.DisplayName}, nil
		}
	}
	if len(status.Status.Applications) > 0 {
		return State{Casting: false, AppDisplayName: status.Status.Applications[0].DisplayName}, nil
	}
	return State{Casting: false}, nil
}

// launch starts (or re-foregrounds) the DashCast receiver app and returns
// its transport id, used to address app-specific messages.
func (cl *Client) launch() (transportID string, err error) {
	msg, err := cl.receiverRequest(map[string]any{
		"type":  "LAUNCH",
		"appId": DashCastAppID,
	})
	if err != nil {
		return "", err
	}

	var status receiverStatusPayload
	if err := json.Unmarshal([]byte(msg.PayloadUTF8), &status); err != nil {
		return "", err
	}
	if status.Type == "LAUNCH_ERROR" {
		return "", errLaunchFailed
	}
	for _, app := range status.Status.Applications {
		if app.AppID == DashCastAppID {
			return app.TransportID, nil
		}
	}
	return "", errLaunchFailed
}

// Start launches DashCast (if needed) and loads targetURL. Because the
// target page (immich-kiosk with cast_fix enabled) is expected to keep the
// Cast session alive itself, we never ask DashCast to periodically reload
// the page.
func (cl *Client) Start(targetURL string) error {
	if err := cl.ensureConnected(); err != nil {
		return err
	}

	transportID, err := cl.launch()
	if err != nil {
		return err
	}

	if err := cl.c.ensureVirtualConnection(transportID); err != nil {
		return err
	}

	return cl.c.send(transportID, nsDashCast, map[string]any{
		"url":         targetURL,
		"force":       true,
		"reload":      false,
		"reload_time": 0,
	})
}

// Stop stops the currently running receiver app (DashCast), ending the
// casting session. STOP requires the current session id, so we fetch fresh
// status first.
func (cl *Client) Stop() error {
	if err := cl.ensureConnected(); err != nil {
		return err
	}

	msg, err := cl.receiverRequest(map[string]any{"type": "GET_STATUS"})
	if err != nil {
		return err
	}
	var status receiverStatusPayload
	if err := json.Unmarshal([]byte(msg.PayloadUTF8), &status); err != nil {
		return err
	}

	var sessionID string
	for _, app := range status.Status.Applications {
		if app.AppID == DashCastAppID {
			sessionID = app.SessionID
		}
	}
	if sessionID == "" {
		// Nothing to stop.
		return nil
	}

	_, err = cl.receiverRequest(map[string]any{"type": "STOP", "sessionId": sessionID})
	return err
}
