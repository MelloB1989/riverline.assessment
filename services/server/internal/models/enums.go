package models

type AgentID string

type BorrowerStatus string

type Outcome string

type OfferStatus string

type Persona string

type MessageRole string

type FlagType string

type ComplianceRule string

const (
	AgentAria  AgentID = "aria"
	AgentNova  AgentID = "nova"
	AgentDelta AgentID = "delta"
)

const (
	BorrowerStatusPending       BorrowerStatus = "pending"
	BorrowerStatusInAssessment  BorrowerStatus = "in_assessment"
	BorrowerStatusInResolution  BorrowerStatus = "in_resolution"
	BorrowerStatusInFinalNotice BorrowerStatus = "in_final_notice"
	BorrowerStatusResolved      BorrowerStatus = "resolved"
	BorrowerStatusEscalated     BorrowerStatus = "escalated"
	BorrowerStatusStopContact   BorrowerStatus = "stop_contact"
	BorrowerStatusHardship      BorrowerStatus = "hardship"
)

const (
	OutcomeCommitted   Outcome = "committed"
	OutcomeRejected    Outcome = "rejected"
	OutcomeNoResponse  Outcome = "no_response"
	OutcomeHardship    Outcome = "hardship"
	OutcomeStopContact Outcome = "stop_contact"
	OutcomeEscalated   Outcome = "escalated"
)

const (
	OfferStatusProposed OfferStatus = "proposed"
	OfferStatusAccepted OfferStatus = "accepted"
	OfferStatusRejected OfferStatus = "rejected"
)

const (
	PersonaCooperative Persona = "cooperative"
	PersonaCombative   Persona = "combative"
	PersonaEvasive     Persona = "evasive"
	PersonaDistressed  Persona = "distressed"
	PersonaConfused    Persona = "confused"
)

const (
	MessageRoleAgent    MessageRole = "agent"
	MessageRoleBorrower MessageRole = "borrower"
)

const (
	FlagTypeScoreInflation         FlagType = "score_inflation"
	FlagTypeMetricUselessness      FlagType = "metric_uselessness"
	FlagTypeJudgeDisagreement      FlagType = "judge_disagreement"
	FlagTypeComplianceBlindspot    FlagType = "compliance_blindspot"
	FlagTypePostAdoptionRegression FlagType = "post_adoption_regression"
)

const (
	ComplianceRuleIdentityDisclosure    ComplianceRule = "identity_disclosure"
	ComplianceRuleNoFalseThreats        ComplianceRule = "no_false_threats"
	ComplianceRuleNoHarassment          ComplianceRule = "no_harassment"
	ComplianceRuleNoMisleadingTerms     ComplianceRule = "no_misleading_terms"
	ComplianceRuleSensitiveSituations   ComplianceRule = "sensitive_situations"
	ComplianceRuleRecordingDisclosure   ComplianceRule = "recording_disclosure"
	ComplianceRuleProfessionalComposure ComplianceRule = "professional_composure"
	ComplianceRuleDataPrivacy           ComplianceRule = "data_privacy"
)
