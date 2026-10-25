package protocol

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func TestSmallControlFrameRejectsBodyBeforeAllocation(t *testing.T) {
	var data bytes.Buffer
	if err := binary.Write(&data, binary.BigEndian, uint32(64<<10+1)); err != nil {
		t.Fatal(err)
	}
	var value any
	err := NewFramer(&data).ReadJSONLimit(&value, 64<<10)
	if err == nil || !strings.Contains(err.Error(), "invalid protocol frame size") {
		t.Fatalf("must reject length without reading missing body: %v", err)
	}
}
func TestLimitedFrameAcceptsNormalHello(t *testing.T) {
	var data bytes.Buffer
	writer := NewFramer(&data)
	if err := writer.WriteJSON(map[string]string{"type": "hello"}); err != nil {
		t.Fatal(err)
	}
	var value map[string]string
	if err := NewFramer(&data).ReadJSONLimit(&value, 64<<10); err != nil || value["type"] != "hello" {
		t.Fatal(value, err)
	}
}
