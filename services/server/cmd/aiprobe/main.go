package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"riverline_server/constants"
	"riverline_server/internal/collections"

	"github.com/MelloB1989/karma/ai"
	karmaModels "github.com/MelloB1989/karma/models"
	"github.com/openai/openai-go/v3/shared"
)

func main() {
	mode := "all"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	switch mode {
	case "persona":
		probePersona()
	case "groq":
		probeGroq()
	case "xai":
		probeXAI()
	default:
		probePersona()
		probeGroq()
		probeXAI()
	}
}

func probePersona() {
	cfg := constants.DefaultSelfLearningConfig()
	if strings.TrimSpace(cfg.PersonaLLMAPIKey) == "" {
		log.Println("[probe persona] PERSONA_LLM_API_KEY missing; skipping")
		return
	}
	client := collections.NewLLMClient(cfg.PersonaLLMBaseURL, cfg.PersonaLLMAPIKey, cfg.PersonaLLMModel)
	messages := []collections.LlmMessage{
		{Role: "system", Content: "You are a borrower talking to Riverline collections. Reply naturally."},
		{Role: "user", Content: "Stage: aria. Reply briefly."},
	}
	for attempt := 1; attempt <= 3; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		start := time.Now()
		resp, err := client.ChatWithTokenUsage(ctx, messages, 0.3, 256)
		cancel()
		if err != nil {
			log.Printf("[probe persona] attempt=%d duration=%s err=%v", attempt, time.Since(start), err)
			continue
		}
		log.Printf("[probe persona] attempt=%d ok duration=%s tokens_in=%d tokens_out=%d stop_reason=%s chars=%d text=%q",
			attempt, time.Since(start), resp.InputTokens, resp.OutputTokens, resp.StopReason, len(resp.Content), preview(resp.Content, 200))
		return
	}
}

func probeGroq() {
	client := ai.NewKarmaAI(
		ai.GPTOSS_120B,
		ai.Groq,
		ai.WithSystemMessage("You are a strict JSON generator. Reply with valid JSON only."),
		ai.WithMaxTokens(200),
		ai.WithTemperature(0.1),
	)
	probeKarma("groq/gpt-oss-120b", client)
	probeKarmaLargePromptGen()
}

func probeKarmaLargePromptGen() {
	cfg := constants.DefaultSelfLearningConfig()
	maxTokens := cfg.PromptGeneratorMaxTokens
	if maxTokens <= 0 {
		maxTokens = 2200
	}
	system := "You are Riverline's internal prompt optimization service. Output only the replacement system prompt, around 1500 tokens, no markdown."
	prompt := "Generate an improved production system prompt for the aria collections agent.\n\nCurrent prompt:\n" + strings.Repeat("You are Aria, the assessment agent. Verify identity, gather facts. ", 50) + "\n\nQuantitative evidence:\n" + strings.Repeat("- workflow=A persona=cooperative score=42.0 compliance=0.0 disagreement=20.0\n", 30) + "\nReturn only the complete replacement system prompt."
	options := []ai.Option{
		ai.WithSystemMessage(system),
		ai.WithMaxTokens(maxTokens),
		ai.WithTemperature(0.15),
	}
	if strings.TrimSpace(cfg.PromptGenerator.ReasoningEffort) != "" && maxTokens >= 4000 {
		options = append(options, ai.WithReasoningEffort(shared.ReasoningEffort(cfg.PromptGenerator.ReasoningEffort)))
	}
	client := ai.NewKarmaAI(
		ai.BaseModel(cfg.PromptGenerator.Model),
		ai.Provider(cfg.PromptGenerator.Provider),
		options...,
	)
	for attempt := 1; attempt <= 3; attempt++ {
		start := time.Now()
		resp, err := client.GenerateFromSinglePrompt(prompt)
		if err != nil {
			log.Printf("[probe groq large-prompt] attempt=%d duration=%s err=%v", attempt, time.Since(start), err)
			continue
		}
		log.Printf("[probe groq large-prompt] attempt=%d duration=%s tokens_in=%d tokens_out=%d chars=%d preview=%q",
			attempt, time.Since(start), resp.InputTokens, resp.OutputTokens, len(resp.AIResponse), preview(resp.AIResponse, 120))
	}
}

func probeXAI() {
	client := ai.NewKarmaAI(
		ai.Grok4ReasoningFast,
		ai.XAI,
		ai.WithSystemMessage("You are a strict JSON generator. Reply with valid JSON only."),
		ai.WithMaxTokens(200),
		ai.WithTemperature(0.1),
	)
	probeKarma("xai/grok-4-fast-reasoning", client)
}

func probeKarma(label string, client *ai.KarmaAI) {
	prompt := "Return JSON {\"score\": 7, \"reasoning\": \"This is a probe.\"}. Only JSON."

	for attempt := 1; attempt <= 2; attempt++ {
		start := time.Now()
		resp, err := client.GenerateFromSinglePrompt(prompt)
		if err != nil {
			log.Printf("[probe %s GenerateFromSinglePrompt] attempt=%d duration=%s err=%v", label, attempt, time.Since(start), err)
		} else {
			log.Printf("[probe %s GenerateFromSinglePrompt] attempt=%d duration=%s tokens_in=%d tokens_out=%d chars=%d text=%q",
				label, attempt, time.Since(start), resp.InputTokens, resp.OutputTokens, len(resp.AIResponse), preview(resp.AIResponse, 200))
		}
	}

	for attempt := 1; attempt <= 2; attempt++ {
		hist := &karmaModels.AIChatHistory{
			Messages: []karmaModels.AIMessage{
				{UniqueId: fmt.Sprintf("probe-%d", attempt), Role: karmaModels.User, Message: prompt, Timestamp: time.Now().UTC()},
			},
		}
		start := time.Now()
		resp, err := client.ChatCompletionManaged(hist)
		if err != nil {
			log.Printf("[probe %s ChatCompletionManaged] attempt=%d duration=%s err=%v", label, attempt, time.Since(start), err)
			continue
		}
		log.Printf("[probe %s ChatCompletionManaged] attempt=%d duration=%s tokens_in=%d tokens_out=%d chars=%d text=%q",
			label, attempt, time.Since(start), resp.InputTokens, resp.OutputTokens, len(resp.AIResponse), preview(resp.AIResponse, 200))
	}
}

func preview(value string, maxLen int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > maxLen {
		return value[:maxLen] + "..."
	}
	return value
}
