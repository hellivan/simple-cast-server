package chromecast

import "errors"

var (
	errInvalidProtobuf = errors.New("chromecast: invalid protobuf CastMessage")
	errNotConnected    = errors.New("chromecast: not connected")
	errTimeout         = errors.New("chromecast: timed out waiting for response")
	errLaunchFailed    = errors.New("chromecast: app failed to launch")
)
