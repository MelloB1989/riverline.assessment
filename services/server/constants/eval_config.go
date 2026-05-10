package constants

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/MelloB1989/karma/ai"
)

type EvaluatorJudgeConfig struct {
	Name        string  `json:"name"`
	Provider    string  `json:"provider"`
	Model       string  `json:"model"`
	Weight      float64 `json:"weight"`
	Temperature float32 `json:"temperature"`
}

type SelfLearningConfig struct {
	Judges                  []EvaluatorJudgeConfig `json:"judges"`
	PromptGenerator         EvaluatorJudgeConfig   `json:"prompt_generator"`
	PersonaLLMBaseURL       string                 `json:"persona_llm_base_url"`
	PersonaLLMAPIKey        string                 `json:"-"`
	PersonaLLMModel         string                 `json:"persona_llm_model"`
	DefaultBatchSize        int                    `json:"default_batch_size"`          //default number of simulations per persona when running evals.
	DefaultMaxTurnsPerAgent int                    `json:"default_max_turns_per_agent"` //hard cap on how many turns each agent gets in simulation.
	AdoptionPValue          float64                `json:"adoption_p_value"`            //statistical significance threshold for prompt adoption.
	AdoptionMinMeanDelta    float64                `json:"adoption_min_mean_delta"`     //minimum mean improvement required before a prompt can be adopted.
	AdoptionMinCohensD      float64                `json:"adoption_min_cohens_d"`       // minimum effect size required before adoption.
	AdoptionMaxStddev       float64                `json:"adoption_max_stddev"`         //upper bound on treatment score spread, to avoid adopting noisy prompts.
	MinComplianceRate       float64                `json:"min_compliance_rate"`         //minimum compliance rate required for adoption.
	MaxJudgeDisagreement    float64                `json:"max_judge_disagreement"`      //threshold used to flag excessive judge disagreement.
	MetaEvaluationMinSample int                    `json:"meta_evaluation_min_sample"`  //minimum sample size before the meta-evaluator starts flagging issues.
	MaxPromptIterations     int                    `json:"max_prompt_iterations"`       //maximum prompt generate/evaluate attempts per learning cycle.
	MetaEvalEveryJudgeRuns  int                    `json:"meta_eval_every_judge_runs"`  //run meta-evaluator after this many LLM judge calls.
}

type ModelPricing struct {
	InputPerMillion  float64 `json:"input_per_million"`
	OutputPerMillion float64 `json:"output_per_million"`
}

func DefaultSelfLearningConfig() SelfLearningConfig {
	cfg := AppCfg.Get()
	out := SelfLearningConfig{
		Judges: []EvaluatorJudgeConfig{
			{Name: "judge_a", Provider: string(ai.Groq), Model: string(ai.Llama31_8B), Weight: 1, Temperature: 1},
			{Name: "judge_b", Provider: string(ai.Groq), Model: string(ai.GPTOSS_120B), Weight: 1, Temperature: 1},
			{Name: "judge_c", Provider: string(ai.XAI), Model: string(ai.Grok4ReasoningFast), Weight: 1, Temperature: 1},
			{Name: "judge_d", Provider: string(ai.Groq), Model: string(ai.Llama33_70B), Weight: 1, Temperature: 1},
		},
		PromptGenerator:         EvaluatorJudgeConfig{Name: "prompt_generator", Provider: firstNonEmpty(cfg.PromptGenProvider, string(ai.Groq)), Model: firstNonEmpty(cfg.PromptGenModel, string(ai.GPTOSS_120B)), Weight: 1, Temperature: 0.2},
		PersonaLLMBaseURL:       strings.TrimRight(cfg.PersonaLLMBaseURL, "/"),
		PersonaLLMAPIKey:        cfg.PersonaLLMApiKey,
		PersonaLLMModel:         cfg.PersonaLLMModel,
		DefaultBatchSize:        2,
		DefaultMaxTurnsPerAgent: 6,
		AdoptionPValue:          0.05,
		AdoptionMinMeanDelta:    5,
		AdoptionMinCohensD:      0.35,
		AdoptionMaxStddev:       25,
		MinComplianceRate:       1,
		MaxJudgeDisagreement:    20,
		MetaEvaluationMinSample: 5,
		MaxPromptIterations:     3,
		MetaEvalEveryJudgeRuns:  6,
	}
	raw := strings.TrimSpace(firstNonEmpty(cfg.EvaluatorJudges, os.Getenv("EVALUATOR_JUDGES_JSON")))
	if raw != "" {
		var judges []EvaluatorJudgeConfig
		if err := json.Unmarshal([]byte(raw), &judges); err == nil && len(judges) > 0 {
			out.Judges = normalizeJudgeConfig(judges)
		}
	}
	out.Judges = normalizeJudgeConfig(out.Judges)
	out.PromptGenerator = normalizeJudgeConfig([]EvaluatorJudgeConfig{out.PromptGenerator})[0]
	return out
}

func EstimateLLMCostUSD(modelUsed string, promptTokens, completionTokens int) float64 {
	pricing := defaultModelPricing()
	raw := strings.TrimSpace(firstNonEmpty(AppCfg.Get().LlmPricing, os.Getenv("LLM_PRICING_JSON")))
	if raw != "" {
		var override map[string]ModelPricing
		if err := json.Unmarshal([]byte(raw), &override); err == nil {
			for model, price := range override {
				pricing[model] = price
			}
		}
	}
	price, ok := pricing[modelUsed]
	if !ok {
		for model, candidate := range pricing {
			if strings.Contains(modelUsed, model) {
				price = candidate
				ok = true
				break
			}
		}
	}
	if !ok {
		return 0
	}
	inputCost := (float64(promptTokens) / 1_000_000) * price.InputPerMillion
	outputCost := (float64(completionTokens) / 1_000_000) * price.OutputPerMillion
	return inputCost + outputCost
}

func defaultModelPricing() map[string]ModelPricing {
	return map[string]ModelPricing{
		"groq/llama-3.3-70b-versatile": {InputPerMillion: 0.59, OutputPerMillion: 0.79},
		"llama-3.3-70b-versatile":      {InputPerMillion: 0.59, OutputPerMillion: 0.79},
		"claude-3-5-haiku-20241022":    {InputPerMillion: 0.80, OutputPerMillion: 4.00},
		"claude-3.5-haiku":             {InputPerMillion: 0.80, OutputPerMillion: 4.00},
		"openai/gpt-oss-120b":          {InputPerMillion: 0.15, OutputPerMillion: 0.60},
		"groq/openai/gpt-oss-120b":     {InputPerMillion: 0.15, OutputPerMillion: 0.60},
		"openai/gpt-oss-20b":           {InputPerMillion: 0.075, OutputPerMillion: 0.30},
		"groq/openai/gpt-oss-20b":      {InputPerMillion: 0.075, OutputPerMillion: 0.30},
		"grok-4-fast-reasoning":        {InputPerMillion: 0.20, OutputPerMillion: 0.50},
		"xai/grok-4-fast-reasoning":    {InputPerMillion: 0.20, OutputPerMillion: 0.50},
	}
}

func normalizeJudgeConfig(judges []EvaluatorJudgeConfig) []EvaluatorJudgeConfig {
	out := make([]EvaluatorJudgeConfig, 0, len(judges))
	for i, judge := range judges {
		if strings.TrimSpace(judge.Provider) == "" {
			judge.Provider = "groq"
		}
		if strings.TrimSpace(judge.Model) == "" {
			judge.Model = "llama-3.3-70b"
		}
		if strings.TrimSpace(judge.Name) == "" {
			judge.Name = "judge_" + string(rune('a'+i))
		}
		if judge.Weight <= 0 {
			judge.Weight = 1
		}
		out = append(out, judge)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
