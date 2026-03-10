package main

import (
	"context"
	"io"
	"log"
	"os"

	"google.golang.org/genai"
)

const defaultModel = "gemini-2.0-flash-live-001"

type GeminiCallbacks struct {
	OnAudio      func(data []byte)
	OnText       func(text string)
	SetSpeaking  func(speaking bool)
}

func getAPIKey() string {
	if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		return key
	}
	return "你的 API KEY"
}

func startGemini(ctx context.Context, cb GeminiCallbacks) error {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  getAPIKey(),
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return err
	}

	config := &genai.LiveConnectConfig{
		ResponseModalities: []genai.Modality{genai.ModalityAudio},
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{{Text: "你是小爱音箱，请用中文回答用户的问题。"}},
		},
		SpeechConfig: &genai.SpeechConfig{
			LanguageCode: "cmn-CN",
			VoiceConfig: &genai.VoiceConfig{
				PrebuiltVoiceConfig: &genai.PrebuiltVoiceConfig{VoiceName: "Leda"},
			},
		},
	}

	session, err := client.Live.Connect(ctx, defaultModel, config)
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
