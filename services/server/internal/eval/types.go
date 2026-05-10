package eval

import (
	"riverline_server/constants"
	"riverline_server/internal/models"
)

type SimConfig struct {
	Seed                   int64                             `json:"seed"`
	BatchSize              int                               `json:"batch_size"`
	Personas               []models.Persona                  `json:"personas"`
	AgentID                models.AgentID                    `json:"agent_id"`
	MaxTurnsPerAgent       int                               `json:"max_turns_per_agent"`
	Judges                 []constants.EvaluatorJudgeConfig  `json:"judges"`
	PromptOverrides        map[models.AgentID]PromptOverride `json:"-"`
	MaxRunCostUSD          float64                           `json:"max_run_cost_usd"`
	BaseRunCostUSD         float64                           `json:"base_run_cost_usd"`
	MaxPromptIterations    int                               `json:"max_prompt_iterations"`
	MetaEvalEveryJudgeRuns int                               `json:"meta_eval_every_judge_runs"`
	PersonaGuidance        string                            `json:"persona_guidance,omitempty"`
}

type PromptOverride struct {
	VersionNumber int    `json:"version_number"`
	PromptText    string `json:"prompt_text"`
}

type SimulatedConversation struct {
	Workflow         models.BorrowerWorkflow    `json:"workflow"`
	Conversation     models.AgentConversation   `json:"conversation"`
	Conversations    []models.AgentConversation `json:"conversations"`
	Transcript       string                     `json:"transcript"`
	AgentTranscripts map[models.AgentID]string  `json:"agent_transcripts"`
	Persona          models.Persona             `json:"persona"`
	Seed             string                     `json:"seed"`
	Metadata         map[string]any             `json:"metadata,omitempty"`
}

type MetricScores struct {
	CompositeScore      float64        `json:"composite_score"`
	IdentityVerified    float64        `json:"identity_verified"`
	InfoCompleteness    float64        `json:"info_completeness"`
	NoRedundancy        float64        `json:"no_redundancy"`
	ToneAppropriateness float64        `json:"tone_appropriateness"`
	OfferClarity        float64        `json:"offer_clarity"`
	ObjectionHandling   float64        `json:"objection_handling"`
	CommitmentAttempt   float64        `json:"commitment_attempt"`
	ContextContinuity   float64        `json:"context_continuity"`
	ConsequenceAccuracy float64        `json:"consequence_accuracy"`
	DeadlineSpecificity float64        `json:"deadline_specificity"`
	NoNegotiationDrift  float64        `json:"no_negotiation_drift"`
	CompliancePass      float64        `json:"compliance_pass"`
	ComplianceBreakdown map[string]any `json:"compliance_breakdown"`
	JudgeBComposite     float64        `json:"judge_b_composite"`
	JudgeDisagreement   float64        `json:"judge_disagreement_delta"`
	Reasoning           string         `json:"reasoning"`
}

type EvaluationResult struct {
	Metrics          MetricScores
	EvaluatorVersion models.EvaluatorVersion
	Tokens           int
	InputTokens      int
	OutputTokens     int
	ModelUsed        string
	JudgeResults     []JudgeResult
}

type GeneratedText struct {
	Text         string
	InputTokens  int
	OutputTokens int
	ModelUsed    string
}

type JudgeResult struct {
	Name         string       `json:"name"`
	Provider     string       `json:"provider"`
	Model        string       `json:"model"`
	Weight       float64      `json:"weight"`
	Valid        bool         `json:"valid"`
	Error        string       `json:"error,omitempty"`
	Metrics      MetricScores `json:"metrics"`
	Tokens       int          `json:"tokens"`
	InputTokens  int          `json:"input_tokens"`
	OutputTokens int          `json:"output_tokens"`
	CostUSD      float64      `json:"cost_usd"`
	ModelUsed    string       `json:"model_used"`
}

type SimulationScore struct {
	SimulationSeed    string         `json:"simulation_seed"`
	Persona           models.Persona `json:"persona"`
	WorkflowID        string         `json:"workflow_id"`
	ConversationID    string         `json:"conversation_id"`
	PromptVersion     int            `json:"prompt_version"`
	Scores            []float64      `json:"scores"`
	Mean              float64        `json:"mean"`
	ComplianceRate    float64        `json:"compliance_rate"`
	JudgeDisagreement float64        `json:"judge_disagreement_delta"`
	JudgeResults      []JudgeResult  `json:"judge_results,omitempty"`
	Reasoning         string         `json:"reasoning,omitempty"`
}

type RerunRequest struct {
	ConversationIDs []string                         `json:"conversation_ids"`
	WorkflowIDs     []string                         `json:"workflow_ids"`
	AgentID         *models.AgentID                  `json:"agent_id"`
	Judges          []constants.EvaluatorJudgeConfig `json:"judges"`
}

type RerunResult struct {
	ScoredConversationIDs []string `json:"scored_conversation_ids"`
	ScoreCount            int      `json:"score_count"`
}

type RollbackRequest struct {
	AgentID       models.AgentID `json:"agent_id"`
	VersionNumber int            `json:"version_number"`
	Reason        string         `json:"reason"`
}

type EvalMetrics struct {
	TotalScores       int                        `json:"total_scores"`
	TotalCostUSD      float64                    `json:"total_cost_usd"`
	SystemAggregate   MetricAggregate            `json:"system_aggregate"`
	ByAgentPrompt     map[string]MetricAggregate `json:"by_agent_prompt"`
	PromptExperiments []models.PromptExperiment  `json:"prompt_experiments"`
}

type MetricAggregate struct {
	N                 int     `json:"n"`
	Mean              float64 `json:"mean"`
	Stddev            float64 `json:"stddev"`
	Median            float64 `json:"median"`
	ComplianceRate    float64 `json:"compliance_rate"`
	MeanDisagreement  float64 `json:"mean_judge_disagreement"`
	SimulatedFraction float64 `json:"simulated_fraction"`
}
