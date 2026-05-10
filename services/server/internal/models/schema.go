package models

import (
	"time"
)

type User struct {
	TableName string         `karma_table:"users" json:"-"`
	Id        string         `json:"id" karma:"primary"`
	FirstName string         `json:"first_name"`
	LastName  string         `json:"last_name"`
	Email     string         `json:"email"`
	Phone     *string        `json:"phone"`
	Dob       time.Time      `json:"dob"`
	Gender    string         `json:"gender"`
	IsAdmin   bool           `json:"is_admin"`
	Extra     map[string]any `json:"extra" db:"extra"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
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
	TableName              string     `karma_table:"borrower_workflows" json:"-"`
	Id                     string     `json:"id" karma:"primary"`
	UserId                 string     `json:"user_id"`
	LoanId                 string     `json:"loan_id"`
	CurrentStage           AgentID    `json:"current_stage"`
	AriaAttempts           int        `json:"aria_attempts"`
	Outcome                *Outcome   `json:"outcome"`
	IdentityVerified       *bool      `json:"identity_verified"`
	EmploymentStatus       *string    `json:"employment_status"`
	MonthlyIncomeRange     *string    `json:"monthly_income_range"`
	MonthlyObligations     *float64   `json:"monthly_obligations"`
	DefaultReason          *string    `json:"default_reason"`
	BorrowerEmotionalState *Persona   `json:"borrower_emotional_state"`
	HardshipMentioned      *bool      `json:"hardship_mentioned"`
	AriaSummary            *string    `json:"aria_summary"`
	ContextForNova         *string    `json:"context_for_nova"`
	ContextForDelta        *string    `json:"context_for_delta"`
	FinalOfferAmount       *float64   `json:"final_offer_amount"`
	FinalOfferDeadline     *time.Time `json:"final_offer_deadline"`
	ResolvedAt             *time.Time `json:"resolved_at"`
	StopContactFlagged     *bool      `json:"stop_contact_flagged"`
	HardshipFlagged        *bool      `json:"hardship_flagged"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

type ResolutionOffer struct {
	TableName           string         `karma_table:"resolution_offers" json:"-"`
	Id                  string         `json:"id" karma:"primary"`
	WorkflowId          string         `json:"workflow_id"`
	CandidateOffer      map[string]any `json:"candidate_offer" db:"candidate_offer"`
	Status              OfferStatus    `json:"status"`
	VapiCallId          *string        `json:"vapi_call_id"`
	CallRecordingUrl    *string        `json:"call_recording_url"`
	CallTranscript      *string        `json:"call_transcript"`
	CallDurationSeconds *int           `json:"call_duration_seconds"`
	ScheduledCallAt     *time.Time     `json:"scheduled_call_at"`
	LumpSumOffered      *float64       `json:"lump_sum_offered"`
	LumpSumDiscountPct  *float64       `json:"lump_sum_discount_pct"`
	EmiAmount           *float64       `json:"emi_amount"`
	EmiMonths           *int           `json:"emi_months"`
	EmiStartDate        *time.Time     `json:"emi_start_date"`
	HardshipOffered     *bool          `json:"hardship_offered"`
	OfferAccepted       *bool          `json:"offer_accepted"`
	AcceptedOfferType   *string        `json:"accepted_offer_type"`
	ObjectionsRaised    []string       `json:"objections_raised" db:"objections_raised"`
	CreatedAt           time.Time      `json:"created_at"`
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
	TotalTurns      *int       `json:"total_turns"`
	TotalTokensUsed *int       `json:"total_tokens_used"`
	StartedAt       time.Time  `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at"`
}

type AgentMessage struct {
	TableName      string      `karma_table:"agent_messages" json:"-"`
	Id             string      `json:"id" karma:"primary"`
	ConversationId string      `json:"conversation_id"`
	WorkflowId     string      `json:"workflow_id"`
	AgentId        AgentID     `json:"agent_id"`
	Role           MessageRole `json:"role"`
	Content        string      `json:"content"`
	TokenCount     *int        `json:"token_count"`
	CreatedAt      time.Time   `json:"created_at"`
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
	ControlComplianceRate   float64   `json:"control_compliance_rate"`
	ControlScores           []float64 `json:"control_scores" db:"control_scores"`
	TreatmentN              int       `json:"treatment_n"`
	TreatmentMean           float64   `json:"treatment_mean"`
	TreatmentStddev         float64   `json:"treatment_stddev"`
	TreatmentMedian         float64   `json:"treatment_median"`
	TreatmentComplianceRate float64   `json:"treatment_compliance_rate"`
	TreatmentScores         []float64 `json:"treatment_scores" db:"treatment_scores"`
	MeanDelta               float64   `json:"mean_delta"`
	PValue                  float64   `json:"p_value"`
	CohensD                 *float64  `json:"cohens_d"`
	IsSignificant           *bool     `json:"is_significant"`
	Adopted                 bool      `json:"adopted"`
	RejectionReason         *string   `json:"rejection_reason"`
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
