package main

import "testing"

func TestDefaultConfigInterruptsStopWordsWithoutCallingAI(t *testing.T) {
	cfg := defaultConfig()

	if !cfg.ShouldInterrupt("闭嘴") {
		t.Fatal("expected stop word to interrupt playback")
	}
	if cfg.shouldCallAI("闭嘴") {
		t.Fatal("expected stop word to avoid AI response")
	}
}

func TestCallAIKeywordsTriggerAIWithoutInterruptKeyword(t *testing.T) {
	cfg := defaultConfig()
	cfg.Interrupt.Keywords = []string{"闭嘴", "停止"}
	cfg.CallAIKeywords = []string{"请", "你"}

	if !cfg.ShouldInterrupt("请你帮我讲一个睡前故事") {
		t.Fatal("expected call AI text to enter interruption flow even when interrupt keywords are stop-only")
	}
	if !cfg.shouldCallAI("请你帮我讲一个睡前故事") {
		t.Fatal("expected call_ai_keywords to trigger AI")
	}
}

func TestInstructionDecisionPrefersCallAIOverStopOnlyInterruptGate(t *testing.T) {
	cfg := defaultConfig()
	cfg.Interrupt.Keywords = []string{"闭嘴", "停止"}
	cfg.CallAIKeywords = []string{"请", "你"}

	decision, keyword := cfg.instructionDecision("请你帮我讲一个睡前故事")
	if decision != instructionDecisionCallAI {
		t.Fatalf("expected call AI decision, got %v", decision)
	}
	if keyword != "请" {
		t.Fatalf("expected call AI keyword 请, got %q", keyword)
	}
}

func TestInstructionDecisionStopWordInterruptsOnly(t *testing.T) {
	cfg := defaultConfig()
	cfg.Interrupt.Keywords = []string{"闭嘴", "停止"}
	cfg.CallAIKeywords = []string{"请", "你"}

	decision, keyword := cfg.instructionDecision("闭嘴")
	if decision != instructionDecisionInterruptOnly {
		t.Fatalf("expected interrupt-only decision, got %v", decision)
	}
	if keyword != "闭嘴" {
		t.Fatalf("expected interrupt keyword 闭嘴, got %q", keyword)
	}
}

func TestInstructionDecisionCustomReplyBypassesCallAIKeywords(t *testing.T) {
	cfg := defaultConfig()
	cfg.CallAIKeywords = []string{"豆包", "小智", "小度"}
	cfg.CustomReplies = []CustomReply{
		{Match: "测试播放文字", Text: "你好，很高兴认识你！"},
	}

	decision, keyword := cfg.instructionDecision("测试播放文字")
	if decision != instructionDecisionCallAI {
		t.Fatalf("expected custom reply to enter message handling, got %v", decision)
	}
	if keyword != "custom_reply" {
		t.Fatalf("expected custom reply keyword marker, got %q", keyword)
	}
}

func TestCustomReplyRequiresSynchronousStopBeforePlayback(t *testing.T) {
	cfg := defaultConfig()
	cfg.CustomReplies = []CustomReply{
		{Match: "测试播放文字", Text: "你好，很高兴认识你！"},
	}

	if !cfg.shouldStopTTSBeforeHandling("测试播放文字") {
		t.Fatal("expected custom reply playback to wait for stop_tts before starting")
	}
	if cfg.shouldStopTTSBeforeHandling("豆包讲个故事") {
		t.Fatal("expected normal AI request to keep asynchronous stop_tts")
	}
}
