package audio

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testUpwardPacket(kind uint64, fields ...[]byte) []byte {
	body := protoMessage(protoVarintField(1, kind))
	body = append(body, protoMessage(fields...)...)
	return protoMessage(protoVarintField(1, speechUpward), protoField(2, body))
}

func receiveDownward(t *testing.T, conn *net.UnixConn) ([]protoFieldValue, []protoFieldValue) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 4096)
	n, _, err := conn.ReadFromUnix(buffer)
	if err != nil {
		t.Fatalf("read downward packet: %v", err)
	}
	outer, err := parseProtoFields(buffer[:n])
	if err != nil {
		t.Fatalf("parse outer packet: %v", err)
	}
	body, ok := protoBytes(outer, 3)
	if !ok {
		t.Fatal("downward packet is missing body")
	}
	fields, err := parseProtoFields(body)
	if err != nil {
		t.Fatalf("parse downward body: %v", err)
	}
	return outer, fields
}

func TestSpeechUsockRegistrationPcmAndControl(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "us-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	serverPath := filepath.Join(dir, "speech.usock")
	clientPath := filepath.Join(dir, "mipns.usock")
	server, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: serverPath, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: clientPath, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	audio := make(chan []byte, 1)
	events := make(chan string, 4)
	service := &speechUsock{
		conn: server,
		callback: usockCallbacks{
			onAudio: func(data []byte) error { audio <- append([]byte(nil), data...); return nil },
			onEvent: func(name string, _ interface{}) { events <- name },
		},
		registered: make(chan error, 1),
	}
	peer := &net.UnixAddr{Name: clientPath, Net: "unixgram"}

	registerBody := protoMessage(protoField(1, []byte("xiaomi")), protoField(2, []byte("pcm")))
	if err := service.handlePacket(testUpwardPacket(upRegister, protoField(2, registerBody)), peer); err != nil {
		t.Fatalf("handle register: %v", err)
	}
	if _, fields := receiveDownward(t, client); protoVarint(fields, 1) != downRegisterResponse {
		t.Fatal("expected registration response")
	}
	if _, fields := receiveDownward(t, client); protoVarint(fields, 1) != downEnableVoiceWakeup {
		t.Fatal("expected voice wakeup enable response")
	}

	prepareBody := protoMessage(protoVarintField(1, 0), protoVarintField(2, 1))
	if err := service.handlePacket(testUpwardPacket(upStreamPrepare, protoField(3, prepareBody)), peer); err != nil {
		t.Fatalf("handle stream prepare: %v", err)
	}
	_, prepare := receiveDownward(t, client)
	if protoVarint(prepare, 1) != downStreamPrepareResponse {
		t.Fatal("expected stream prepare response")
	}
	if got := <-events; got != "kws" {
		t.Fatalf("expected wake event, got %q", got)
	}

	pcm := []byte{0x01, 0x02, 0x03, 0x04}
	transmitting := protoMessage(protoVarintField(1, streamASR), protoField(2, pcm))
	if err := service.handlePacket(testUpwardPacket(upStreamTransmitting, protoField(4, transmitting)), peer); err != nil {
		t.Fatalf("handle PCM packet: %v", err)
	}
	if got := <-audio; string(got) != string(pcm) {
		t.Fatalf("PCM mismatch: %v", got)
	}
	if err := service.FinishDialog(true); err != nil {
		t.Fatalf("continue dialog: %v", err)
	}
	if _, fields := receiveDownward(t, client); protoVarint(fields, 1) != downExpectSpeech {
		t.Fatal("expected expect-speech response")
	}
	if _, fields := receiveDownward(t, client); protoVarint(fields, 1) != downDialogFinish {
		t.Fatal("expected follow-up dialog finish response")
	}

	if err := service.StopCapture(); err != nil {
		t.Fatalf("stop capture: %v", err)
	}
	if _, fields := receiveDownward(t, client); protoVarint(fields, 1) != downStopCapture {
		t.Fatal("expected stop capture response")
	}
	if err := service.FinishDialog(false); err != nil {
		t.Fatalf("finish dialog: %v", err)
	}
	if _, fields := receiveDownward(t, client); protoVarint(fields, 1) != downDialogFinish {
		t.Fatal("expected dialog finish response")
	}
}

func TestSpeechUsockRejectsOpusRegistration(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "us-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	serverPath := filepath.Join(dir, "speech.usock")
	server, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: serverPath, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	service := &speechUsock{conn: server, registered: make(chan error, 1)}
	peer := &net.UnixAddr{Name: filepath.Join(dir, "mipns.usock"), Net: "unixgram"}

	registerBody := protoMessage(protoField(1, []byte("xiaomi")), protoField(2, []byte("opus32")))
	err = service.handlePacket(testUpwardPacket(upRegister, protoField(2, registerBody)), peer)
	if err == nil || !strings.Contains(err.Error(), "without -r opus32") {
		t.Fatalf("expected actionable Opus rejection, got %v", err)
	}
}

func TestCaptureBackendDefaultsToNoopAndAcceptsUsock(t *testing.T) {
	if DefaultAudioConfig.Backend != "noop" {
		t.Fatalf("default capture backend = %q, want noop", DefaultAudioConfig.Backend)
	}
	var config AudioConfig
	if err := json.Unmarshal([]byte(`{"backend":"usock"}`), &config); err != nil {
		t.Fatalf("parse capture backend: %v", err)
	}
	if config.Backend != "usock" {
		t.Fatalf("capture backend = %q, want usock", config.Backend)
	}
}

func TestHasMountTargetMatchesExactMountPoint(t *testing.T) {
	mounts := "rootfs / ext4 rw 0 0\n/dev/sda /tmp/mico_aivs_lab/usock/speech.usock tmpfs rw 0 0\n"
	if !hasMountTarget(mounts, speechSocketPath) {
		t.Fatal("expected exact speech socket mount to match")
	}
	if hasMountTarget(mounts, "/tmp/mico_aivs_lab/usock/common.usock") {
		t.Fatal("a different socket mount must not match")
	}
}

func TestStopRecordingSendsStopAndFinishesDialog(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "us-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	serverPath := filepath.Join(dir, "speech.usock")
	clientPath := filepath.Join(dir, "mipns.usock")
	server, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: serverPath, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: clientPath, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	service := &speechUsock{conn: server, peer: &net.UnixAddr{Name: clientPath, Net: "unixgram"}, dialogID: "dialog"}
	recorder := &AudioRecorder{recording: true, backend: "usock", usock: service}
	if err := recorder.StopRecording(); err != nil {
		t.Fatalf("stop recording: %v", err)
	}
	if _, fields := receiveDownward(t, client); protoVarint(fields, 1) != downStopCapture {
		t.Fatal("expected stop-capture packet")
	}
	if _, fields := receiveDownward(t, client); protoVarint(fields, 1) != downDialogFinish {
		t.Fatal("expected dialog-finish packet")
	}
}

func TestFollowupCaptureRequestValidatesResponse(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		want string
	}{
		{name: "success", body: `{"code":0}`},
		{name: "rejected", body: `{"code":-1}`, want: "rejected"},
		{name: "invalid json", body: `not-json`, want: "parse pnshelper"},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := writeFakeCommand(t, test.body)
			err := requestFollowupCaptureWithCommand(command)
			if test.want == "" && err != nil {
				t.Fatalf("expected success, got %v", err)
			}
			if test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("expected error containing %q, got %v", test.want, err)
			}
		})
	}
}

func writeFakeCommand(t *testing.T, response string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ubus")
	script := "#!/bin/sh\nprintf '%s\\n' '" + response + "'\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDecodeProtoFieldsRejectsTruncatedInput(t *testing.T) {
	if _, err := parseProtoFields([]byte{0x0a, 0x05, 0x01}); err == nil {
		t.Fatal("expected truncated length-delimited field error")
	}
	if _, err := parseProtoFields([]byte{0x80}); err == nil {
		t.Fatal("expected truncated varint error")
	}
}
