package monitor

import (
	"encoding/json"
	"testing"
)

func TestFileMonitorEventMarshalNewLineMatchesRustClient(t *testing.T) {
	data, err := json.Marshal(FileMonitorEvent{Type: "NewLine", Line: "hello"})
	if err != nil {
		t.Fatalf("marshal FileMonitorEvent: %v", err)
	}

	want := `{"NewLine":"hello"}`
	if string(data) != want {
		t.Fatalf("expected %s, got %s", want, data)
	}
}

func TestFileMonitorEventMarshalNewFileMatchesRustClient(t *testing.T) {
	data, err := json.Marshal(FileMonitorEvent{Type: "NewFile"})
	if err != nil {
		t.Fatalf("marshal FileMonitorEvent: %v", err)
	}

	want := `"NewFile"`
	if string(data) != want {
		t.Fatalf("expected %s, got %s", want, data)
	}
}
