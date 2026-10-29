package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"
	"os/exec"
	"sync"
)

const a113CaptureBitsPerSample = 32

// chunkPool 复用录音 chunk 缓冲，减少老设备上的 GC 压力
var chunkPool = sync.Pool{
	New: func() interface{} {
		// DefaultAudioConfig: BufferSize=1280, PeriodSize=320, S32=4B, 1ch -> targetSize=1280*4=5120
		return make([]byte, 8192) // 略大于典型 targetSize，兼容不同配置
	},
}

// readBufPool 复用 readLoop 的读取缓冲
var readBufPool = sync.Pool{
	New: func() interface{} { return make([]byte, 4096) },
}

// s16ChunkPool 复用 S32->S16 转换输出缓冲
var s16ChunkPool = sync.Pool{
	New: func() interface{} { return make([]byte, 4096) },
}

type AudioRecorder struct {
	mu              sync.Mutex
	cmd             *exec.Cmd
	stopCh          chan struct{}
	recording       bool
	backend         string
	defaultBackend  string
	usock           *speechUsock
	usockOnStream   func([]byte) error
	requestedConfig AudioConfig
}

var (
	recorder     *AudioRecorder
	recorderOnce sync.Once
)

func GetRecorder() *AudioRecorder {
	recorderOnce.Do(func() {
		recorder = &AudioRecorder{defaultBackend: DefaultAudioConfig.Backend}
	})
	return recorder
}

func (r *AudioRecorder) SetDefaultBackend(backend string) error {
	if backend != "noop" && backend != "usock" {
		return fmt.Errorf("unsupported capture backend %q (expected noop or usock)", backend)
	}
	r.mu.Lock()
	r.defaultBackend = backend
	r.mu.Unlock()
	return nil
}

func (r *AudioRecorder) DefaultBackend() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.defaultBackend
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
		if requestedCfg.PCM == "" {
			requestedCfg.PCM = DefaultAudioConfig.PCM
		}
		if requestedCfg.Channels == 0 {
			requestedCfg.Channels = DefaultAudioConfig.Channels
		}
		if requestedCfg.BitsPerSample == 0 {
			requestedCfg.BitsPerSample = DefaultAudioConfig.BitsPerSample
		}
		if requestedCfg.SampleRate == 0 {
			requestedCfg.SampleRate = DefaultAudioConfig.SampleRate
		}
		if requestedCfg.PeriodSize == 0 {
			requestedCfg.PeriodSize = DefaultAudioConfig.PeriodSize
		}
		if requestedCfg.BufferSize == 0 {
			requestedCfg.BufferSize = DefaultAudioConfig.BufferSize
		}
	}
	if requestedCfg.Backend == "" {
		requestedCfg.Backend = r.defaultBackend
	}
	r.requestedConfig = requestedCfg
	if requestedCfg.Backend != "noop" && requestedCfg.Backend != "usock" {
		return fmt.Errorf("unsupported capture backend %q (expected noop or usock)", requestedCfg.Backend)
	}
	if requestedCfg.Backend == "usock" {
		if r.usock == nil {
			return fmt.Errorf("usock capture requested but speech.usock is not initialized")
		}
		r.backend = "usock"
		r.usockOnStream = onStream
		r.recording = true
		return nil
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
	r.backend = "noop"
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
	buf := readBufPool.Get().([]byte)
	if cap(buf) < readSize {
		buf = make([]byte, readSize)
	} else {
		buf = buf[:readSize]
	}
	defer readBufPool.Put(buf)

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
			chunk := chunkPool.Get().([]byte)
			if cap(chunk) < targetSize {
				chunk = make([]byte, targetSize)
			} else {
				chunk = chunk[:targetSize]
			}
			copy(chunk, accumulated[:targetSize])
			accumulated = accumulated[targetSize:]

			transformed, release := transformStreamChunk(chunk, requestedCfg, captureCfg)
			if len(transformed) > 0 {
				onStream(transformed)
			}
			release()
			chunkPool.Put(chunk)
		}
	}

	r.StopRecording()
}

func (r *AudioRecorder) StopRecording() error {
	r.mu.Lock()

	if !r.recording {
		r.usockOnStream = nil
		r.mu.Unlock()
		return nil
	}
	if r.backend == "usock" {
		r.recording = false
		r.usockOnStream = nil
		service := r.usock
		r.mu.Unlock()
		if service != nil {
			if err := service.StopCapture(); err != nil {
				return err
			}
			return service.FinishDialog(false)
		}
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
	r.backend = ""
	r.mu.Unlock()
	return nil
}

func (r *AudioRecorder) EnableUsock(onEvent func(string, interface{}), onFallback func()) error {
	r.mu.Lock()
	if r.usock != nil {
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()

	service, err := startSpeechUsock(usockCallbacks{
		onAudio: func(data []byte) error {
			r.mu.Lock()
			callback := r.usockOnStream
			r.mu.Unlock()
			if callback == nil {
				return nil
			}
			return callback(data)
		},
		onEvent: onEvent,
		onFatal: func(fatal error) {
			log.Printf("❌ speech.usock stopped; falling back to noop capture: %v", fatal)
			go func() {
				r.mu.Lock()
				fallbackStream := r.usockOnStream
				fallbackConfig := r.requestedConfig
				r.mu.Unlock()
				r.DisableUsock()
				fallbackConfig.Backend = "noop"
				if fallbackStream != nil {
					if err := r.StartRecording(fallbackStream, &fallbackConfig); err != nil {
						log.Printf("❌ noop capture fallback failed: %v", err)
					}
				}
				if onFallback != nil {
					onFallback()
				}
			}()
		},
	})
	if err != nil {
		r.mu.Lock()
		r.defaultBackend = "noop"
		r.mu.Unlock()
		return err
	}

	r.mu.Lock()
	r.usock = service
	r.defaultBackend = "usock"
	r.mu.Unlock()
	return nil
}

func (r *AudioRecorder) ContinueDialog() error {
	r.mu.Lock()
	service := r.usock
	r.mu.Unlock()
	if service == nil {
		return nil
	}
	return service.FinishDialog(true)
}

func (r *AudioRecorder) FinishDialog() error {
	r.mu.Lock()
	service := r.usock
	r.mu.Unlock()
	if service == nil {
		return nil
	}
	return service.FinishDialog(false)
}

func (r *AudioRecorder) DisableUsock() {
	r.mu.Lock()
	service := r.usock
	r.usock = nil
	r.defaultBackend = "noop"
	r.recording = false
	r.usockOnStream = nil
	r.mu.Unlock()
	if service != nil {
		service.Stop()
	}
}

func captureConfigForRecording(requested AudioConfig) AudioConfig {
	capture := requested
	if requested.BitsPerSample == 16 {
		capture.BitsPerSample = a113CaptureBitsPerSample
	}
	return capture
}

func transformStreamChunk(chunk []byte, requested, capture AudioConfig) ([]byte, func()) {
	if requested.BitsPerSample != 16 || capture.BitsPerSample != a113CaptureBitsPerSample {
		return chunk, func() {}
	}
	return convertA113S32ToS16(chunk)
}

// convertA113S32ToS16 converts A113 PDM S32_LE data to S16_LE.
// A113 PDM data lives in lower 24 bits of S32_LE: shift right by 8, clamp to int16.
// 返回的 release 必须在 onStream 返回后调用，用于将缓冲归还对象池。
func convertA113S32ToS16(chunk []byte) ([]byte, func()) {
	if len(chunk)%4 != 0 {
		return nil, nil
	}

	frameCount := len(chunk) / 4
	outSize := frameCount * 2
	buf := s16ChunkPool.Get().([]byte)
	if cap(buf) < outSize {
		buf = make([]byte, outSize)
		return fillS16FromChunk(chunk, buf[:outSize]), func() {}
	}
	buf = buf[:outSize]
	return fillS16FromChunk(chunk, buf), func() { s16ChunkPool.Put(buf) }
}

func fillS16FromChunk(chunk, out []byte) []byte {
	frameCount := len(chunk) / 4
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
