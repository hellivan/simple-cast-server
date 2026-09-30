package chromecast

import "testing"

func TestCastMessageRoundTrip(t *testing.T) {
	original := &castMessage{
		ProtocolVersion: 0,
		SourceID:        "sender-0",
		DestinationID:   "receiver-0",
		Namespace:       "urn:x-cast:com.google.cast.receiver",
		PayloadUTF8:     `{"type":"GET_STATUS","requestId":42}`,
	}

	encoded := original.marshal()
	decoded, err := unmarshalCastMessage(encoded)
	if err != nil {
		t.Fatalf("unmarshalCastMessage() error = %v", err)
	}

	if decoded.SourceID != original.SourceID {
		t.Errorf("SourceID = %q, want %q", decoded.SourceID, original.SourceID)
	}
	if decoded.DestinationID != original.DestinationID {
		t.Errorf("DestinationID = %q, want %q", decoded.DestinationID, original.DestinationID)
	}
	if decoded.Namespace != original.Namespace {
		t.Errorf("Namespace = %q, want %q", decoded.Namespace, original.Namespace)
	}
	if decoded.PayloadUTF8 != original.PayloadUTF8 {
		t.Errorf("PayloadUTF8 = %q, want %q", decoded.PayloadUTF8, original.PayloadUTF8)
	}
}

func TestCastMessageRoundTripUnicodePayload(t *testing.T) {
	original := &castMessage{
		SourceID:      "sender-0",
		DestinationID: "receiver-0",
		Namespace:     "urn:x-cast:com.madmod.dashcast",
		PayloadUTF8:   `{"url":"https://example.com/path?x=✓","force":true,"reload":false,"reload_time":0}`,
	}

	encoded := original.marshal()
	decoded, err := unmarshalCastMessage(encoded)
	if err != nil {
		t.Fatalf("unmarshalCastMessage() error = %v", err)
	}
	if decoded.PayloadUTF8 != original.PayloadUTF8 {
		t.Errorf("PayloadUTF8 = %q, want %q", decoded.PayloadUTF8, original.PayloadUTF8)
	}
}

func TestUnmarshalCastMessageInvalid(t *testing.T) {
	if _, err := unmarshalCastMessage([]byte{0xFF}); err == nil {
		t.Error("expected error for truncated varint, got nil")
	}
}
