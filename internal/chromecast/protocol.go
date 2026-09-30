// Package chromecast implements just enough of the Google Cast V2 protocol
// and the DashCast receiver app protocol to launch a URL on a Chromecast (or
// Google Nest Hub) device. It intentionally avoids pulling in a full
// Chromecast SDK (mDNS discovery, media playback, etc.) since all we need is
// "cast this website" / "stop casting" / "get status".
package chromecast

import (
	"encoding/binary"
)

// wire types used by the CastMessage protobuf message.
const (
	wireVarint = 0
	wireBytes  = 2
)

// Field numbers of the extensions.api.cast_channel.CastMessage protobuf
// message, as defined by the Cast V2 protocol.
// See: https://github.com/home-assistant-libs/pychromecast (cast_channel.proto)
const (
	fieldProtocolVersion = 1
	fieldSourceID        = 2
	fieldDestinationID   = 3
	fieldNamespace       = 4
	fieldPayloadType     = 5
	fieldPayloadUTF8     = 6
	fieldPayloadBinary   = 7
)

// PayloadType enum values of CastMessage.
const (
	payloadTypeString = 0
	payloadTypeBinary = 1
)

// castMessage mirrors the fields of the CastMessage protobuf message that we
// care about. Only string payloads are supported, which is all that is
// required to talk to the receiver, connection and DashCast namespaces.
type castMessage struct {
	ProtocolVersion int32
	SourceID        string
	DestinationID   string
	Namespace       string
	PayloadUTF8     string
}

func putUvarint(buf []byte, v uint64) []byte {
	tmp := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(tmp, v)
	return append(buf, tmp[:n]...)
}

func appendTag(buf []byte, fieldNumber int, wireType int) []byte {
	return putUvarint(buf, uint64(fieldNumber)<<3|uint64(wireType))
}

func appendString(buf []byte, fieldNumber int, s string) []byte {
	buf = appendTag(buf, fieldNumber, wireBytes)
	buf = putUvarint(buf, uint64(len(s)))
	return append(buf, s...)
}

func appendVarint(buf []byte, fieldNumber int, v int64) []byte {
	buf = appendTag(buf, fieldNumber, wireVarint)
	return putUvarint(buf, uint64(v))
}

// marshal encodes the message using the protobuf wire format.
func (m *castMessage) marshal() []byte {
	buf := make([]byte, 0, len(m.PayloadUTF8)+len(m.Namespace)+len(m.SourceID)+len(m.DestinationID)+32)
	buf = appendVarint(buf, fieldProtocolVersion, int64(m.ProtocolVersion))
	buf = appendString(buf, fieldSourceID, m.SourceID)
	buf = appendString(buf, fieldDestinationID, m.DestinationID)
	buf = appendString(buf, fieldNamespace, m.Namespace)
	buf = appendVarint(buf, fieldPayloadType, payloadTypeString)
	buf = appendString(buf, fieldPayloadUTF8, m.PayloadUTF8)
	return buf
}

// unmarshal decodes a CastMessage from the protobuf wire format. Unknown
// fields and binary payloads are ignored.
func unmarshalCastMessage(data []byte) (*castMessage, error) {
	m := &castMessage{}
	i := 0
	for i < len(data) {
		tag, n := binary.Uvarint(data[i:])
		if n <= 0 {
			return nil, errInvalidProtobuf
		}
		i += n
		fieldNumber := int(tag >> 3)
		wireType := int(tag & 0x7)

		switch wireType {
		case wireVarint:
			v, n := binary.Uvarint(data[i:])
			if n <= 0 {
				return nil, errInvalidProtobuf
			}
			i += n
			if fieldNumber == fieldProtocolVersion {
				m.ProtocolVersion = int32(v)
			}
		case wireBytes:
			l, n := binary.Uvarint(data[i:])
			if n <= 0 {
				return nil, errInvalidProtobuf
			}
			i += n
			if i+int(l) > len(data) {
				return nil, errInvalidProtobuf
			}
			value := data[i : i+int(l)]
			i += int(l)

			switch fieldNumber {
			case fieldSourceID:
				m.SourceID = string(value)
			case fieldDestinationID:
				m.DestinationID = string(value)
			case fieldNamespace:
				m.Namespace = string(value)
			case fieldPayloadUTF8:
				m.PayloadUTF8 = string(value)
			}
		default:
			return nil, errInvalidProtobuf
		}
	}
	return m, nil
}
