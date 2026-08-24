package protocol

import (
	"bytes"
	"testing"
)

func TestFramerRoundTrip(t *testing.T) {
	stream := &bytes.Buffer{}
	writer := NewFramer(stream)
	if err := writer.WriteJSON(Marker{Type: "ready"}); err != nil {
		t.Fatal(err)
	}
	reader := NewFramer(stream)
	var marker Marker
	if err := reader.ReadJSON(&marker); err != nil {
		t.Fatal(err)
	}
	if marker.Type != "ready" {
		t.Fatalf("unexpected marker: %#v", marker)
	}
}
