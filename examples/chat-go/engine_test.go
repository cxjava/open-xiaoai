package main

import (
	"encoding/json"
	"testing"
)

func TestInstructionLineFromEventDataSupportsNewLine(t *testing.T) {
	line := `{"header":{"namespace":"SpeechRecognizer","name":"RecognizeResult"},"payload":{"is_final":true,"results":[{"text":"请介绍一下你自己"}]}}`
	raw := []byte(`{"NewLine":"` + escapeJSONStringForTest(line) + `"}`)

	got := instructionLineFromEventData(raw)
	if got != line {
		t.Fatalf("expected NewLine field, got %q", got)
	}
}

func escapeJSONStringForTest(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}
