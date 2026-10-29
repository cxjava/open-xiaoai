package audio

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	speechSocketPath = "/tmp/mico_aivs_lab/usock/speech.usock"
	usockRuntimeDir  = "/tmp/open-xiaoai-usock"
	usockScriptPath  = usockRuntimeDir + "/takeover.sh"
	maxSpeechPacket  = 256 * 1024

	speechUpward   = 0
	speechDownward = 1

	upRegister           = 0
	upStreamPrepare      = 1
	upStreamCancel       = 2
	upStreamTransmitting = 3
	upStreamEnd          = 4

	downRegisterResponse      = 0
	downStreamPrepareResponse = 1
	downStopCapture           = 2
	downExpectSpeech          = 3
	downDialogFinish          = 5
	downEnableVoiceWakeup     = 9

	streamWakeup    = 0
	streamWakeupEnd = 1
	streamASR       = 2
)

//go:embed usock_takeover.sh
var usockTakeoverScript string

type usockCallbacks struct {
	onAudio func([]byte) error
	onEvent func(string, interface{})
	onFatal func(error)
}

type speechUsock struct {
	conn           *net.UnixConn
	cancel         chan struct{}
	closed         chan struct{}
	closeOnce      sync.Once
	fatalOnce      sync.Once
	stopped        atomic.Bool
	stateMu        sync.Mutex
	callback       usockCallbacks
	turnID         uint64
	dialogID       string
	peer           *net.UnixAddr
	registered     chan error
	chatCount      uint64
	dialogCount    uint64
	requestCapture func() error
}

func startSpeechUsock(callback usockCallbacks) (*speechUsock, error) {
	if os.Geteuid() != 0 {
		return nil, errors.New("speech.usock takeover requires root")
	}
	if err := os.MkdirAll(usockRuntimeDir, 0700); err != nil {
		return nil, fmt.Errorf("create speech runtime directory: %w", err)
	}
	if err := os.WriteFile(usockScriptPath, []byte(usockTakeoverScript), 0700); err != nil {
		return nil, fmt.Errorf("install runtime speech takeover helper: %w", err)
	}
	if err := runTakeover("prepare"); err != nil {
		return nil, err
	}

	addr := &net.UnixAddr{Name: speechSocketPath, Net: "unixgram"}
	conn, err := net.ListenUnixgram("unixgram", addr)
	if err != nil {
		_ = runTakeover("restore")
		return nil, fmt.Errorf("bind speech.usock: %w", err)
	}
	service := &speechUsock{
		conn:           conn,
		cancel:         make(chan struct{}),
		closed:         make(chan struct{}),
		callback:       callback,
		registered:     make(chan error, 1),
		requestCapture: requestFollowupCapture,
	}
	go service.readLoop()

	if err := runTakeover("protect", fmt.Sprint(os.Getpid())); err != nil {
		service.Stop()
		return nil, err
	}
	go service.watchdog()

	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	select {
	case err := <-service.registered:
		if err != nil {
			service.Stop()
			return nil, fmt.Errorf("speech.usock registration: %w", err)
		}
	case <-timer.C:
		service.Stop()
		return nil, errors.New("timed out waiting for mipns registration on speech.usock")
	case <-service.closed:
		service.Stop()
		select {
		case err := <-service.registered:
			if err != nil {
				return nil, fmt.Errorf("speech.usock registration: %w", err)
			}
		default:
			return nil, errors.New("speech.usock service stopped before registration")
		}
	}
	return service, nil
}

func runTakeover(args ...string) error {
	cmd := exec.Command("sh", append([]string{usockScriptPath}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("speech takeover %s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	if text := strings.TrimSpace(string(output)); text != "" {
		log.Printf("🎙️ speech takeover: %s", text)
	}
	return nil
}

func (s *speechUsock) readLoop() {
	defer close(s.closed)
	buffer := make([]byte, maxSpeechPacket)
	for {
		_ = s.conn.SetReadDeadline(time.Now().Add(time.Second))
		n, peer, err := s.conn.ReadFromUnix(buffer)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				select {
				case <-s.cancel:
					return
				default:
					continue
				}
			}
			select {
			case <-s.cancel:
				return
			default:
			}
			fatal := fmt.Errorf("receive speech.usock datagram: %w", err)
			s.setRegistrationError(fatal)
			log.Printf("❌ speech.usock receive failed: %v", fatal)
			s.signalFatal(fatal)
			return
		}
		if peer == nil || peer.Name == "" {
			continue
		}
		if err := s.handlePacket(buffer[:n], peer); err != nil {
			log.Printf("⚠️ ignored speech.usock packet: %v", err)
		}
	}
}

func (s *speechUsock) watchdog() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.cancel:
			return
		case <-ticker.C:
			if s.stopped.Load() {
				return
			}
			if _, err := os.Stat(speechSocketPath); err != nil {
				fatal := fmt.Errorf("speech.usock socket disappeared: %w", err)
				s.signalFatal(fatal)
				return
			}
			data, err := os.ReadFile("/proc/mounts")
			if err != nil || !hasMountTarget(string(data), speechSocketPath) {
				fatal := errors.New("speech.usock protection mount disappeared")
				s.signalFatal(fatal)
				return
			}
		}
	}
}

func hasMountTarget(mounts, target string) bool {
	for _, line := range strings.Split(mounts, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == target {
			return true
		}
	}
	return false
}

func (s *speechUsock) signalFatal(err error) {
	if s.stopped.Load() {
		return
	}
	s.fatalOnce.Do(func() {
		if s.callback.onFatal != nil {
			s.callback.onFatal(err)
		}
	})
}

func (s *speechUsock) setRegistrationError(err error) {
	select {
	case s.registered <- err:
	default:
	}
}

func (s *speechUsock) handlePacket(packet []byte, peer *net.UnixAddr) error {
	outer, err := parseProtoFields(packet)
	if err != nil {
		return err
	}
	if protoVarint(outer, 1) != speechUpward {
		return nil
	}
	body, ok := protoBytes(outer, 2)
	if !ok {
		return errors.New("upward packet has no body")
	}
	fields, err := parseProtoFields(body)
	if err != nil {
		return err
	}
	kind := protoVarint(fields, 1)
	if kind == ^uint64(0) {
		return errors.New("upward packet has no type")
	}

	s.stateMu.Lock()
	registeredPeer := ""
	if s.peer != nil {
		registeredPeer = s.peer.Name
	}
	s.stateMu.Unlock()
	if kind != upRegister && registeredPeer != peer.Name {
		return fmt.Errorf("ignored message from unregistered peer %q", peer.Name)
	}

	switch kind {
	case upRegister:
		if codecBytes, found := protoNestedBytes(fields, 2, 2); found {
			codec := strings.ToLower(strings.TrimSpace(string(codecBytes)))
			if codec != "" && codec != "pcm" && codec != "pcm16" && codec != "s16le" {
				err := fmt.Errorf("mipns requested unsupported codec %q; configure pns to send PCM without -r opus32", codec)
				s.setRegistrationError(err)
				return err
			}
		}
		s.stateMu.Lock()
		s.peer = &net.UnixAddr{Name: peer.Name, Net: "unixgram"}
		s.stateMu.Unlock()
		if err := s.send(downRegisterResponse, nil, nil); err != nil {
			s.setRegistrationError(err)
			return err
		}
		if err := s.send(downEnableVoiceWakeup, nil, nil); err != nil {
			s.setRegistrationError(err)
			return err
		}
		log.Printf("✅ mipns registered on speech.usock")
		select {
		case s.registered <- nil:
		default:
		}
	case upStreamPrepare:
		select {
		case <-s.cancel:
			return errors.New("speech service is shutting down")
		default:
		}
		activation, _ := protoNestedVarint(fields, 3, 1)
		interaction, _ := protoNestedVarint(fields, 3, 2)
		s.stateMu.Lock()
		s.turnID++
		turnID := s.turnID
		s.dialogID = randomDialogID()
		dialogID := s.dialogID
		if activation == 0 {
			s.dialogCount++
			s.chatCount = 1
		} else {
			s.chatCount++
		}
		s.stateMu.Unlock()
		response := protoMessage(protoBool(1, true))
		if err := s.send(downStreamPrepareResponse, []byte(dialogID), protoField(3, response)); err != nil {
			return err
		}
		if s.callback.onEvent != nil {
			if activation == 0 {
				s.callback.onEvent("kws", map[string]string{"Type": "Keyword"})
			}
			typeName := "FollowUp"
			if activation == 0 {
				typeName = "Keyword"
			}
			s.callback.onEvent("speech", map[string]interface{}{
				"type": typeName, "turn_id": turnID, "interaction_mode": interaction,
			})
		}
	case upStreamTransmitting:
		transmitType, hasType := protoNestedVarint(fields, 4, 1)
		data, hasData := protoNestedBytes(fields, 4, 2)
		if hasType && hasData {
			if transmitType == streamASR && s.callback.onAudio != nil {
				if err := s.callback.onAudio(data); err != nil {
					log.Printf("⚠️ send speech PCM stream: %v", err)
				}
			} else if transmitType == streamWakeup || transmitType == streamWakeupEnd {
				log.Printf("🎙️ speech wakeup packet (%d bytes)", len(data))
			}
		}
	case upStreamCancel, upStreamEnd:
		if s.callback.onEvent != nil {
			typeName := "End"
			if kind == upStreamCancel {
				typeName = "Cancelled"
			}
			s.stateMu.Lock()
			turnID := s.turnID
			s.stateMu.Unlock()
			s.callback.onEvent("speech", map[string]interface{}{"type": typeName, "turn_id": turnID})
		}
	default:
		log.Printf("⚠️ unknown speech.usock message type %d", kind)
	}
	return nil
}

func (s *speechUsock) send(kind uint64, dialogID []byte, payload []byte) error {
	if kind == downRegisterResponse || kind == downEnableVoiceWakeup {
		return s.sendDownward(protoVarintField(1, kind))
	}
	parts := [][]byte{protoVarintField(1, kind)}
	if dialogID != nil {
		parts = append(parts, protoField(2, dialogID))
	}
	if payload != nil {
		parts = append(parts, payload)
	}
	return s.sendDownward(parts...)
}

func (s *speechUsock) sendDownward(parts ...[]byte) error {
	s.stateMu.Lock()
	peer := s.peer
	s.stateMu.Unlock()
	if peer == nil {
		return errors.New("mipns has not registered")
	}
	packet := protoMessage(protoVarintField(1, speechDownward), protoField(3, protoMessage(parts...)))
	n, err := s.conn.WriteToUnix(packet, peer)
	if err != nil {
		return fmt.Errorf("send speech.usock response: %w", err)
	}
	if n != len(packet) {
		return fmt.Errorf("short speech.usock datagram write: wrote %d of %d", n, len(packet))
	}
	return nil
}

func (s *speechUsock) StopCapture() error {
	s.stateMu.Lock()
	dialogID := s.dialogID
	s.stateMu.Unlock()
	parts := [][]byte{protoVarintField(1, downStopCapture)}
	if dialogID != "" {
		parts = append(parts, protoField(2, []byte(dialogID)))
	}
	return s.sendDownward(parts...)
}

func (s *speechUsock) FinishDialog(continueDialog bool) error {
	s.stateMu.Lock()
	dialogID := s.dialogID
	chatCount, dialogCount := s.chatCount, s.dialogCount
	s.stateMu.Unlock()
	if dialogID == "" {
		return nil
	}
	if continueDialog {
		expect := protoMessage(protoBool(2, false))
		if err := s.send(downExpectSpeech, []byte(dialogID), protoField(4, expect)); err != nil {
			return err
		}
	}
	finish := protoMessage(
		protoBool(1, false),
		protoBool(2, continueDialog),
		protoBool(3, continueDialog),
		protoBool(4, !continueDialog),
		protoVarintField(5, chatCount),
		protoVarintField(6, dialogCount),
	)
	if err := s.send(downDialogFinish, []byte(dialogID), protoField(6, finish)); err != nil {
		return err
	}
	if continueDialog && s.requestCapture != nil {
		time.Sleep(100 * time.Millisecond)
		if err := s.requestCapture(); err != nil {
			return fmt.Errorf("request follow-up capture after sending dialog finish: %w", err)
		}
	}
	return nil
}

func requestFollowupCapture() error {
	return requestFollowupCaptureWithCommand("ubus")
}

func requestFollowupCaptureWithCommand(command string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, "-t", "1", "-S", "call", "pnshelper", "event_notify", `{"src":1,"event":0}`)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pnshelper follow-up request: %w: %s", err, strings.TrimSpace(string(output)))
	}
	var response struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		return fmt.Errorf("parse pnshelper follow-up response: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if response.Code != 0 {
		return fmt.Errorf("pnshelper rejected follow-up capture: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func (s *speechUsock) Stop() {
	s.closeOnce.Do(func() {
		s.stopped.Store(true)
		close(s.cancel)
		_ = s.conn.Close()
		_ = runTakeover("restore")
		select {
		case <-s.closed:
		case <-time.After(2 * time.Second):
		}
	})
}

func randomDialogID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		copy(b[:], []byte(fmt.Sprintf("%016x", time.Now().UnixNano())))
	}
	return fmt.Sprintf("%x", b)
}

type protoFieldValue struct {
	number uint64
	wire   uint64
	value  uint64
	bytes  []byte
}

func parseProtoFields(buf []byte) ([]protoFieldValue, error) {
	var fields []protoFieldValue
	for len(buf) > 0 {
		key, rest, err := decodeProtoVarint(buf)
		if err != nil {
			return nil, err
		}
		buf = rest
		number, wire := key>>3, key&7
		if number == 0 {
			return nil, errors.New("protobuf field number 0")
		}
		field := protoFieldValue{number: number, wire: wire}
		switch wire {
		case 0:
			field.value, buf, err = decodeProtoVarint(buf)
			if err != nil {
				return nil, err
			}
		case 1:
			if len(buf) < 8 {
				return nil, errors.New("truncated protobuf fixed64")
			}
			field.bytes, buf = buf[:8], buf[8:]
		case 2:
			length, remaining, readErr := decodeProtoVarint(buf)
			if readErr != nil || length > uint64(len(remaining)) {
				return nil, errors.New("truncated protobuf bytes")
			}
			field.bytes, buf = remaining[:int(length)], remaining[int(length):]
		case 5:
			if len(buf) < 4 {
				return nil, errors.New("truncated protobuf fixed32")
			}
			field.bytes, buf = buf[:4], buf[4:]
		default:
			return nil, fmt.Errorf("unsupported protobuf wire type %d", wire)
		}
		fields = append(fields, field)
	}
	return fields, nil
}

func decodeProtoVarint(buf []byte) (uint64, []byte, error) {
	var value uint64
	for shift := uint(0); shift < 64; shift += 7 {
		if len(buf) == 0 {
			return 0, nil, errors.New("truncated protobuf varint")
		}
		b := buf[0]
		buf = buf[1:]
		value |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return value, buf, nil
		}
	}
	return 0, nil, errors.New("protobuf varint too long")
}

func protoVarint(fields []protoFieldValue, number uint64) uint64 {
	for _, field := range fields {
		if field.number == number && field.wire == 0 {
			return field.value
		}
	}
	return ^uint64(0)
}

func protoBytes(fields []protoFieldValue, number uint64) ([]byte, bool) {
	for _, field := range fields {
		if field.number == number && field.wire == 2 {
			return field.bytes, true
		}
	}
	return nil, false
}

func protoNestedBytes(fields []protoFieldValue, outer, inner uint64) ([]byte, bool) {
	data, ok := protoBytes(fields, outer)
	if !ok {
		return nil, false
	}
	nested, err := parseProtoFields(data)
	if err != nil {
		return nil, false
	}
	return protoBytes(nested, inner)
}

func protoNestedVarint(fields []protoFieldValue, outer, inner uint64) (uint64, bool) {
	data, ok := protoBytes(fields, outer)
	if !ok {
		return 0, false
	}
	nested, err := parseProtoFields(data)
	if err != nil {
		return 0, false
	}
	value := protoVarint(nested, inner)
	return value, value != ^uint64(0)
}

func protoMessage(parts ...[]byte) []byte {
	var out []byte
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}

func protoVarintField(number, value uint64) []byte {
	out := encodeProtoVarint(number << 3)
	return append(out, encodeProtoVarint(value)...)
}

func protoBool(number uint64, value bool) []byte {
	if value {
		return protoVarintField(number, 1)
	}
	return protoVarintField(number, 0)
}

func protoField(number uint64, value []byte) []byte {
	out := encodeProtoVarint(number<<3 | 2)
	out = append(out, encodeProtoVarint(uint64(len(value)))...)
	return append(out, value...)
}

func encodeProtoVarint(value uint64) []byte {
	var out []byte
	for value >= 0x80 {
		out = append(out, byte(value)|0x80)
		value >>= 7
	}
	return append(out, byte(value))
}
