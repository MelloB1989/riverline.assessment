package models

import (
	"time"

	"github.com/MelloB1989/karma/models"
)

type BillingAddress struct {
	Country string `json:"country"`
	State   string `json:"state"`
	City    string `json:"city"`
	Street  string `json:"street"`
	Zipcode string `json:"zipcode"`
}

type User struct {
	TableName      string         `karma_table:"users" json:"-"`
	Id             string         `json:"id" karma:"primary"`
	FirstName      string         `json:"first_name"`
	LastName       string         `json:"last_name"`
	Email          string         `json:"email"`
	Phone          *string        `json:"phone"`
	Dob            time.Time      `json:"dob"`
	Gender         string         `json:"gender"`
	Pfp            *string        `json:"pfp"`
	Bio            string         `json:"bio"`
	Extra          map[string]any `json:"extra" db:"extra"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	BillingAddress BillingAddress `json:"billing_address" db:"billing_address"`
}

type Loan struct {
	TableName            string         `karma_table:"loans" json:"-"`
	Id                   string         `json:"id" karma:"primary"`
	UserId               string         `json:"user_id"`
	AccountNumberPartial string         `json:"account_number_partial"`
	LoanType             string         `json:"loan_type"`
	PrincipalAmount      float64        `json:"principal_amount"`
	OutstandingAmount    float64        `json:"outstanding_amount"`
	DaysOverdue          int            `json:"days_overdue"`
	LastPaymentDate      *time.Time     `json:"last_payment_date"`
	LastPaymentAmount    *float64       `json:"last_payment_amount"`
	InterestRate         *float64       `json:"interest_rate"`
	PolicyMaxDiscountPct float64        `json:"policy_max_discount_pct"`
	Status               BorrowerStatus `json:"status"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
}

type BorrowerWorkflow struct {
	TableName          string     `karma_table:"borrower_workflows" json:"-"`
	Id                 string     `json:"id" karma:"primary"`
	UserId             string     `json:"user_id"`
	LoanId             string     `json:"loan_id"`
	CurrentStage       AgentID    `json:"current_stage"`
	AriaAttempts       int        `json:"aria_attempts"`
	Outcome            *Outcome   `json:"outcome"`
	AriaSummary        *string    `json:"aria_summary"`
	NovaSummary        *string    `json:"nova_summary"`
	FinalOfferAmount   *float64   `json:"final_offer_amount"`
	FinalOfferDeadline *time.Time `json:"final_offer_deadline"`
	ResolvedAt         *time.Time `json:"resolved_at"`
	StopContactFlagged *bool      `json:"stop_contact_flagged"`
	HardshipFlagged    *bool      `json:"hardship_flagged"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type Assessment struct {
	TableName              string    `karma_table:"assessments" json:"-"`
	Id                     string    `json:"id" karma:"primary"`
	WorkflowId             string    `json:"workflow_id"`
	UserId                 string    `json:"user_id"`
	IdentityVerified       *bool     `json:"identity_verified"`
	EmploymentStatus       *string   `json:"employment_status"`
	MonthlyIncomeRange     *string   `json:"monthly_income_range"`
	MonthlyObligations     *float64  `json:"monthly_obligations"`
	DefaultReason          *string   `json:"default_reason"`
	BorrowerEmotionalState *Persona  `json:"borrower_emotional_state"`
	HasSavings             *bool     `json:"has_savings"`
	HardshipMentioned      *bool     `json:"hardship_mentioned"`
	CreatedAt              time.Time `json:"created_at"`
}

type ResolutionOffer struct {
	TableName           string     `karma_table:"resolution_offers" json:"-"`
	Id                  string     `json:"id" karma:"primary"`
	WorkflowId          string     `json:"workflow_id"`
	VapiCallId          *string    `json:"vapi_call_id"`
	CallRecordingUrl    *string    `json:"call_recording_url"`
	CallTranscript      *string    `json:"call_transcript"`
	LumpSumOffered      *float64   `json:"lump_sum_offered"`
	LumpSumDiscountPct  *float64   `json:"lump_sum_discount_pct"`
	EmiAmount           *float64   `json:"emi_amount"`
	EmiMonths           *int       `json:"emi_months"`
	EmiStartDate        *time.Time `json:"emi_start_date"`
	HardshipOffered     *bool      `json:"hardship_offered"`
	OfferAccepted       *bool      `json:"offer_accepted"`
	AcceptedOfferType   *string    `json:"accepted_offer_type"`
	ObjectionsRaised    []string   `json:"objections_raised" db:"objections_raised"`
	CallDurationSeconds *int       `json:"call_duration_seconds"`
	CreatedAt           time.Time  `json:"created_at"`
}

type AgentConversation struct {
	TableName       string     `karma_table:"agent_conversations" json:"-"`
	Id              string     `json:"id" karma:"primary"`
	WorkflowId      string     `json:"workflow_id"`
	UserId          string     `json:"user_id"`
	AgentId         AgentID    `json:"agent_id"`
	IsSimulated     *bool      `json:"is_simulated"`
	PersonaType     *Persona   `json:"persona_type"`
	Seed            *string    `json:"seed"`
	PromptVersion   int        `json:"prompt_version"`
	Outcome         *Outcome   `json:"outcome"`
	Summary         string     `json:"summary"`
	TotalTurns      *int       `json:"total_turns"`
	TotalTokensUsed *int       `json:"total_tokens_used"`
	StartedAt       time.Time  `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at"`
}

type AgentMessage struct {
	TableName      string                  `karma_table:"agent_messages" json:"-"`
	Id             string                  `json:"id" karma:"primary"`
	ConversationId string                  `json:"conversation_id"`
	WorkflowId     string                  `json:"workflow_id"`
	UserId         string                  `json:"user_id"`
	AgentId        AgentID                 `json:"agent_id"`
	Role           MessageRole             `json:"role"`
	Content        string                  `json:"content"`
	ToolCalls      []models.OpenAIToolCall `json:"tools,omitempty"` // Tool calls based on OpenAI standards
	ToolCallId     string                  `json:"tool_call_id,omitempty"`
	Images         []string                `json:"images,omitempty"`
	Files          []string                `json:"files,omitempty"`
	TokenCount     *int                    `json:"token_count"`
	CreatedAt      time.Time               `json:"created_at"`
}

type UserMemory struct {
	TableName     string         `karma_table:"user_memories" json:"-"`
	Id            string         `json:"id" karma:"primary"`
	UserId        string         `json:"user_id"`
	MemoryToc     map[string]any `json:"memory_toc" db:"memory_toc"`
	MemoryTree    map[string]any `json:"memory_tree" db:"memory_tree"`
	TokenEstimate *int           `json:"token_estimate"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type PromptVersion struct {
	TableName       string     `karma_table:"prompt_versions" json:"-"`
	Id              string     `json:"id" karma:"primary"`
	AgentId         AgentID    `json:"agent_id"`
	VersionNumber   int        `json:"version_number"`
	PromptText      string     `json:"prompt_text"`
	IsActive        bool       `json:"is_active"`
	AdoptedAt       *time.Time `json:"adopted_at"`
	RetiredAt       *time.Time `json:"retired_at"`
	AdoptionReason  *string    `json:"adoption_reason"`
	RejectionReason *string    `json:"rejection_reason"`
	CreatedAt       time.Time  `json:"created_at"`
}

type ConversationScore struct {
	TableName                string         `karma_table:"conversation_scores" json:"-"`
	Id                       string         `json:"id" karma:"primary"`
	ConversationId           string         `json:"conversation_id"`
	WorkflowId               *string        `json:"workflow_id"`
	AgentId                  AgentID        `json:"agent_id"`
	PromptVersion            int            `json:"prompt_version"`
	EvaluatorVersion         int            `json:"evaluator_version"`
	IsSimulated              *bool          `json:"is_simulated"`
	PersonaType              *Persona       `json:"persona_type"`
	Seed                     *string        `json:"seed"`
	CompositeScore           float64        `json:"composite_score"`
	ScoreIdentityVerified    *float64       `json:"score_identity_verified"`
	ScoreInfoCompleteness    *float64       `json:"score_info_completeness"`
	ScoreNoRedundancy        *float64       `json:"score_no_redundancy"`
	ScoreToneAppropriateness *float64       `json:"score_tone_appropriateness"`
	ScoreOfferClarity        *float64       `json:"score_offer_clarity"`
	ScoreObjectionHandling   *float64       `json:"score_objection_handling"`
	ScoreCommitmentAttempt   *float64       `json:"score_commitment_attempt"`
	ScoreContextContinuity   *float64       `json:"score_context_continuity"`
	ScoreConsequenceAccuracy *float64       `json:"score_consequence_accuracy"`
	ScoreDeadlineSpecificity *float64       `json:"score_deadline_specificity"`
	ScoreNoNegotiationDrift  *float64       `json:"score_no_negotiation_drift"`
	ScoreCompliancePass      *float64       `json:"score_compliance_pass"`
	ComplianceBreakdown      map[string]any `json:"compliance_breakdown" db:"compliance_breakdown"`
	CompliancePassed         *bool          `json:"compliance_passed"`
	JudgeBComposite          *float64       `json:"judge_b_composite"`
	JudgeBMetricScores       map[string]any `json:"judge_b_metric_scores" db:"judge_b_metric_scores"`
	JudgeDisagreementDelta   *float64       `json:"judge_disagreement_delta"`
	EvalCostUsd              *float64       `json:"eval_cost_usd"`
	EvalModelUsed            *string        `json:"eval_model_used"`
	CreatedAt                time.Time      `json:"created_at"`
}

type PromptExperiment struct {
	TableName               string    `karma_table:"prompt_experiments" json:"-"`
	Id                      string    `json:"id" karma:"primary"`
	AgentId                 AgentID   `json:"agent_id"`
	ControlVersion          int       `json:"control_version"`
	CandidateVersion        int       `json:"candidate_version"`
	ControlN                int       `json:"control_n"`
	ControlMean             float64   `json:"control_mean"`
	ControlStddev           float64   `json:"control_stddev"`
	ControlMedian           float64   `json:"control_median"`
	ControlP10              *float64  `json:"control_p10"`
	ControlP90              *float64  `json:"control_p90"`
	ControlMin              *float64  `json:"control_min"`
	ControlMax              *float64  `json:"control_max"`
	ControlComplianceRate   float64   `json:"control_compliance_rate"`
	TreatmentN              int       `json:"treatment_n"`
	TreatmentMean           float64   `json:"treatment_mean"`
	TreatmentStddev         float64   `json:"treatment_stddev"`
	TreatmentMedian         float64   `json:"treatment_median"`
	TreatmentP10            *float64  `json:"treatment_p10"`
	TreatmentP90            *float64  `json:"treatment_p90"`
	TreatmentMin            *float64  `json:"treatment_min"`
	TreatmentMax            *float64  `json:"treatment_max"`
	TreatmentComplianceRate *float64  `json:"treatment_compliance_rate"`
	MeanDelta               float64   `json:"mean_delta"`
	PValue                  float64   `json:"p_value"`
	CohensD                 *float64  `json:"cohens_d"`
	IsSignificant           *bool     `json:"is_significant"`
	Adopted                 bool      `json:"adopted"`
	RejectionReason         *string   `json:"rejection_reason"`
	ControlScores           []float64 `json:"control_scores" db:"control_scores"`
	TreatmentScores         []float64 `json:"treatment_scores" db:"treatment_scores"`
	ExperimentCostUsd       *float64  `json:"experiment_cost_usd"`
	CreatedAt               time.Time `json:"created_at"`
}

type MetaFlag struct {
	TableName              string         `karma_table:"meta_flags" json:"-"`
	Id                     string         `json:"id" karma:"primary"`
	FlagType               FlagType       `json:"flag_type"`
	AgentId                *AgentID       `json:"agent_id"`
	Evidence               map[string]any `json:"evidence" db:"evidence"`
	ProposedAction         *string        `json:"proposed_action"`
	Resolved               *bool          `json:"resolved"`
	Resolution             *string        `json:"resolution"`
	EvaluatorVersionBefore *int           `json:"evaluator_version_before"`
	EvaluatorVersionAfter  *int           `json:"evaluator_version_after"`
	CreatedAt              time.Time      `json:"created_at"`
	ResolvedAt             *time.Time     `json:"resolved_at"`
}

type EvaluatorVersion struct {
	TableName         string    `karma_table:"evaluator_versions" json:"-"`
	Id                string    `json:"id" karma:"primary"`
	VersionNumber     int       `json:"version_number"`
	AgentId           AgentID   `json:"agent_id"`
	JudgePrompt       string    `json:"judge_prompt"`
	IsActive          *bool     `json:"is_active"`
	ChangeReason      *string   `json:"change_reason"`
	TriggeredByFlagId *string   `json:"triggered_by_flag_id"`
	CreatedAt         time.Time `json:"created_at"`
}

type ComplianceCanary struct {
	TableName   string         `karma_table:"compliance_canaries" json:"-"`
	Id          string         `json:"id" karma:"primary"`
	AgentId     AgentID        `json:"agent_id"`
	Rule        ComplianceRule `json:"rule"`
	Description string         `json:"description"`
	Transcript  string         `json:"transcript"`
	ShouldFail  *bool          `json:"should_fail"`
	CreatedAt   time.Time      `json:"created_at"`
}

type CanaryResult struct {
	TableName        string    `karma_table:"canary_results" json:"-"`
	Id               string    `json:"id" karma:"primary"`
	CanaryId         string    `json:"canary_id"`
	EvaluatorVersion int       `json:"evaluator_version"`
	CheckerResult    *bool     `json:"checker_result"`
	CorrectlyFlagged *bool     `json:"correctly_flagged"`
	CreatedAt        time.Time `json:"created_at"`
}

type LlmCostLog struct {
	TableName        string    `karma_table:"llm_cost_log" json:"-"`
	Id               string    `json:"id" karma:"primary"`
	CallType         string    `json:"call_type"`
	AgentId          *AgentID  `json:"agent_id"`
	ModelUsed        string    `json:"model_used"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	CostUsd          float64   `json:"cost_usd"`
	ConversationId   *string   `json:"conversation_id"`
	ExperimentId     *string   `json:"experiment_id"`
	CreatedAt        time.Time `json:"created_at"`
}

type EvalRun struct {
	TableName             string         `karma_table:"eval_runs" json:"-"`
	Id                    string         `json:"id" karma:"primary"`
	RunLabel              string         `json:"run_label"`
	Seed                  int            `json:"seed"`
	BatchSize             int            `json:"batch_size"`
	PersonasUsed          []Persona      `json:"personas_used" db:"personas_used"`
	AgentIds              []AgentID      `json:"agent_ids" db:"agent_ids"`
	PromptVersionsUsed    map[string]any `json:"prompt_versions_used" db:"prompt_versions_used"`
	EvaluatorVersionsUsed map[string]any `json:"evaluator_versions_used" db:"evaluator_versions_used"`
	TotalConversations    *int           `json:"total_conversations"`
	TotalCostUsd          *float64       `json:"total_cost_usd"`
	StartedAt             time.Time      `json:"started_at"`
	CompletedAt           *time.Time     `json:"completed_at"`
	ConfigSnapshot        map[string]any `json:"config_snapshot" db:"config_snapshot"`
}
