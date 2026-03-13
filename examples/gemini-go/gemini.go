package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/url"

	"google.golang.org/genai"
)

type GeminiCallbacks struct {
	OnAudio     func(data []byte)
	OnText      func(text string)
	SetSpeaking func(speaking bool)
}

func startGemini(ctx context.Context, cfg *AppConfig, cb GeminiCallbacks) error {
	apiKey := cfg.GetAPIKey()
	if apiKey == "" {
		log.Fatal("❌ 请设置 GEMINI_API_KEY 环境变量或在 config.yaml 中配置 gemini.api_key")
	}

	clientCfg := &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	}
	if cfg.Proxy != "" {
		proxyURL, err := url.Parse(cfg.Proxy)
		if err != nil {
			log.Printf("⚠️ 无效的代理地址 %q: %v，将不使用代理", cfg.Proxy, err)
		} else {
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.Proxy = http.ProxyURL(proxyURL)
			clientCfg.HTTPClient = &http.Client{Transport: transport}
			log.Printf("🔗 使用代理: %s", cfg.Proxy)
		}
	}
	client, err := genai.NewClient(ctx, clientCfg)
	if err != nil {
		return err
	}

	sysInst := cfg.Gemini.SystemInstruction
	if sysInst == "" {
		sysInst = "你是小爱音箱，请用中文回答用户的问题。"
	}
	lang := cfg.Gemini.Speech.Language
	if lang == "" {
		lang = "cmn-CN"
	}
	voice := cfg.Gemini.Speech.Voice
	if voice == "" {
		voice = "Leda"
	}

	config := &genai.LiveConnectConfig{
		ResponseModalities: []genai.Modality{genai.ModalityAudio},
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{{Text: sysInst}},
		},
		SpeechConfig: &genai.SpeechConfig{
			LanguageCode: lang,
			VoiceConfig: &genai.VoiceConfig{
				PrebuiltVoiceConfig: &genai.PrebuiltVoiceConfig{VoiceName: voice},
			},
		},
	}

	model := cfg.Gemini.Model
	if model == "" {
		model = "gemini-2.0-flash-live-001"
	}
	session, err := client.Live.Connect(ctx, model, config)
	if err != nil {
		return err
	}
	defer session.Close()

	geminiSession = session
	log.Println("🔊 AI: session connected")

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		log.Println("🔊 AI: waiting for response")

		if cb.SetSpeaking != nil {
			cb.SetSpeaking(false)
		}

		for {
			msg, err := session.Receive()
			if err != nil {
				if err == io.EOF || ctx.Err() != nil {
					return nil
				}
				return err
			}

			if msg.ServerContent == nil {
				continue
			}
			if msg.ServerContent.Interrupted {
				continue
			}

			if cb.SetSpeaking != nil {
				cb.SetSpeaking(true)
			}

			if msg.ServerContent.ModelTurn != nil {
				for _, part := range msg.ServerContent.ModelTurn.Parts {
					if part.InlineData != nil && len(part.InlineData.Data) > 0 {
						log.Printf("🔊 AI: audio %d bytes", len(part.InlineData.Data))
						if cb.OnAudio != nil {
							cb.OnAudio(part.InlineData.Data)
						}
					}
					if part.Text != "" {
						log.Printf("✅ AI: %s", part.Text)
						if cb.OnText != nil {
							cb.OnText(part.Text)
						}
					}
				}
			}

			if msg.ServerContent.TurnComplete {
				break
			}
		}
	}
}

// geminiSession is the active live session (set after Connect).
var geminiSession *genai.Session

func sendAudioToGemini(data []byte) {
	s := geminiSession
	if s == nil {
		return
	}
	err := s.SendRealtimeInput(genai.LiveRealtimeInput{
		Audio: &genai.Blob{
			Data:     data,
			MIMEType: "audio/pcm;rate=16000",
		},
	})
	if err != nil {
		log.Printf("❌ send audio to Gemini: %v", err)
	}
}

// sendTextToGemini sends user text to Gemini (e.g. from instruction event).
// This triggers Gemini to interrupt current response and process the new input.
func sendTextToGemini(text string) {
	s := geminiSession
	if s == nil || text == "" {
		return
	}
	err := s.SendRealtimeInput(genai.LiveRealtimeInput{Text: text})
	if err != nil {
		log.Printf("❌ send text to Gemini: %v", err)
		return
	}
	log.Printf("⏹️ 已发送用户文本打断: %s", text)
}
