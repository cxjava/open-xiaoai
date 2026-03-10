package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os/exec"
	"sync"
)

const a113CaptureBitsPerSample = 32

type AudioRecorder struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	stopCh   chan struct{}
	recording bool
}

var (
	recorder     *AudioRecorder
	recorderOnce sync.Once
)

func GetRecorder() *AudioRecorder {
	recorderOnce.Do(func() {
		recorder = &AudioRecorder{}
	})
	return recorder
}

func (r *AudioRecorder) StartRecording(onStream func(data []byte) error, config *AudioConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.recording {
		return nil
	}

	requestedCfg := DefaultAudioConfig
	if config != nil {
		requestedCfg = *config
	}

	captureCfg := captureConfigForRecording(requestedCfg)

	cmd := exec.Command("arecord",
		"--quiet",
		"-t", "raw",
		"-D", captureCfg.PCM,
		"-f", fmt.Sprintf("S%d_LE", captureCfg.BitsPerSample),
		"-r", fmt.Sprintf("%d", captureCfg.SampleRate),
		"-c", fmt.Sprintf("%d", captureCfg.Channels),
		"--buffer-size", fmt.Sprintf("%d", captureCfg.BufferSize),
		"--period-size", fmt.Sprintf("%d", captureCfg.PeriodSize),
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("arecord stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("arecord start: %w", err)
	}

	r.cmd = cmd
	r.recording = true
	r.stopCh = make(chan struct{})

	go r.readLoop(stdout, onStream, requestedCfg, captureCfg)

	return nil
}

func (r *AudioRecorder) readLoop(stdout io.ReadCloser, onStream func([]byte) error, requestedCfg, captureCfg AudioConfig) {
	bytesPerSample := max(captureCfg.BitsPerSample, 8) / 8
	bytesPerFrame := bytesPerSample * max(captureCfg.Channels, 1)
	targetFrames := max(captureCfg.BufferSize, 1)
	readFrames := max(captureCfg.PeriodSize, 1)
	targetSize := targetFrames * bytesPerFrame
	readSize := readFrames * bytesPerFrame

	accumulated := make([]byte, 0, targetSize*2)
	buf := make([]byte, readSize)

	for {
		select {
		case <-r.stopCh:
			return
		default:
		}

		n, err := stdout.Read(buf)
		if err != nil || n == 0 {
			break
		}

		accumulated = append(accumulated, buf[:n]...)

		for len(accumulated) >= targetSize {
			chunk := make([]byte, targetSize)
			copy(chunk, accumulated[:targetSize])
			accumulated = accumulated[targetSize:]

			transformed := transformStreamChunk(chunk, requestedCfg, captureCfg)
			if len(transformed) > 0 {
				onStream(transformed)
			}
		}
	}

	r.StopRecording()
}

func (r *AudioRecorder) StopRecording() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.recording {
		return nil
	}

	if r.stopCh != nil {
		select {
		case <-r.stopCh:
		default:
			close(r.stopCh)
		}
	}

	if r.cmd != nil {
		r.cmd.Process.Kill()
		r.cmd.Wait()
		r.cmd = nil
	}

	r.recording = false
	return nil
}

func captureConfigForRecording(requested AudioConfig) AudioConfig {
	capture := requested
	if requested.BitsPerSample == 16 {
		capture.BitsPerSample = a113CaptureBitsPerSample
	}
	return capture
}

func transformStreamChunk(chunk []byte, requested, capture AudioConfig) []byte {
	if requested.BitsPerSample != 16 || capture.BitsPerSample != a113CaptureBitsPerSample {
		return chunk
	}
	return convertA113S32ToS16(chunk)
}

// convertA113S32ToS16 converts A113 PDM S32_LE data to S16_LE.
// A113 PDM data lives in lower 24 bits of S32_LE: shift right by 8, clamp to int16.
func convertA113S32ToS16(chunk []byte) []byte {
	if len(chunk)%4 != 0 {
		return nil
	}

	frameCount := len(chunk) / 4
	out := make([]byte, frameCount*2)

	for i := 0; i < frameCount; i++ {
		sample := int32(binary.LittleEndian.Uint32(chunk[i*4 : i*4+4]))
		mapped := sample >> 8
		if mapped > math.MaxInt16 {
			mapped = math.MaxInt16
		} else if mapped < math.MinInt16 {
			mapped = math.MinInt16
		}
		binary.LittleEndian.PutUint16(out[i*2:i*2+2], uint16(int16(mapped)))
	}

	return out
}
