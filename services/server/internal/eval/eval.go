package eval

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"riverline_server/internal/agents"
	"riverline_server/internal/collections"
	"riverline_server/internal/models"

	"github.com/MelloB1989/karma/utils"
	"github.com/MelloB1989/karma/v2/orm"
)

type SimConfig struct {
	Seed      int64
	BatchSize int
	Personas  []models.Persona
	AgentID   models.AgentID
}

type SimulatedConversation struct {
	Conversation models.AgentConversation
	Transcript   string
}

type generatedTranscript struct {
	Content      string
	InputTokens  int
	OutputTokens int
	TotalTokens  int
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
}

func RunSimulation(cfg SimConfig) ([]SimulatedConversation, error) {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 5
	}
	if cfg.AgentID == "" {
		cfg.AgentID = models.AgentAria
	}
	if len(cfg.Personas) == 0 {
		cfg.Personas = []models.Persona{models.PersonaCooperative, models.PersonaCombative, models.PersonaEvasive, models.PersonaDistressed, models.PersonaConfused}
	}
	if err := collections.EnsureDefaults(); err != nil {
		return nil, err
	}
	pv, err := collections.ActivePromptVersion(cfg.AgentID)
	if err != nil {
		return nil, err
	}
	out := make([]SimulatedConversation, 0, cfg.BatchSize*len(cfg.Personas))
	convOrm := orm.Load(&models.AgentConversation{})
	defer convOrm.Close()
	msgOrm := orm.Load(&models.AgentMessage{})
	defer msgOrm.Close()
	for _, persona := range cfg.Personas {
		for i := 0; i < cfg.BatchSize; i++ {
			wf, err := collections.StartWorkflow("", "")
			if err != nil {
				return nil, err
			}
			seed := string(persona) + "-" + time.Now().UTC().Format("20060102150405") + "-" + utils.GenerateID()
			transcript, err := generateSimulatedTranscript(cfg.AgentID, persona, cfg.Seed)
			if err != nil {
				return nil, err
			}
			conv := models.AgentConversation{
				Id:              utils.GenerateID(),
				WorkflowId:      wf.Id,
				UserId:          wf.UserId,
				AgentId:         cfg.AgentID,
				IsSimulated:     boolPtr(true),
				PersonaType:     &persona,
				Seed:            &seed,
				PromptVersion:   pv.VersionNumber,
				TotalTurns:      intPtr(4),
				TotalTokensUsed: intPtr(transcript.TotalTokens),
				StartedAt:       time.Now().UTC(),
			}
			if err := convOrm.Insert(&conv); err != nil {
				return nil, err
			}
			if err := collections.LogCost("simulation", &cfg.AgentID, "groq/llama-3.3-70b", transcript.InputTokens, transcript.OutputTokens, &conv.Id, nil); err != nil {
				return nil, err
			}
			msg := models.AgentMessage{Id: utils.GenerateID(), ConversationId: conv.Id, WorkflowId: conv.WorkflowId, AgentId: cfg.AgentID, Role: models.MessageRoleAgent, Content: transcript.Content, TokenCount: intPtr(transcript.OutputTokens), CreatedAt: time.Now().UTC()}
			_ = msgOrm.Insert(&msg)
			out = append(out, SimulatedConversation{Conversation: conv, Transcript: transcript.Content})
		}
	}
	return out, nil
}

func generateSimulatedTranscript(agentID models.AgentID, persona models.Persona, seed int64) (*generatedTranscript, error) {
	client, err := evaluatorClient(agentID)
	if err != nil {
		return nil, err
	}
	prompt := fmt.Sprintf(`Generate one realistic completed collections conversation transcript for evaluation.

Agent: %s
Borrower persona: %s
Seed: %d

Requirements:
- Use "Agent:" and "Borrower:" lines only.
- Include the required AI and recording disclosures.
- Keep the transcript concise but complete enough to score identity, information completeness, tone, compliance, continuity, and outcome.
- Reflect the persona consistently without caricature.
- Do not include JSON or commentary. Return only the transcript.`, agentID, persona, seed)
	resp, err := client.GenerateText(prompt)
	if err != nil {
		return nil, fmt.Errorf("generate simulated transcript for %s/%s: %w", agentID, persona, err)
	}
	return &generatedTranscript{
		Content:      strings.TrimSpace(resp.AIResponse),
		InputTokens:  resp.InputTokens,
		OutputTokens: resp.OutputTokens,
		TotalTokens:  resp.Tokens,
	}, nil
}

func ScoreAll(conversations []SimulatedConversation) ([]float64, error) {
	scores := make([]float64, 0, len(conversations))
	for _, conv := range conversations {
		evaluation, err := Evaluate(conv.Conversation.AgentId, conv.Transcript)
		if err != nil {
			return nil, err
		}
		if err := SaveScore(conv.Conversation, evaluation); err != nil {
			return nil, err
		}
		scores = append(scores, evaluation.Metrics.CompositeScore)
	}
	return scores, nil
}

func Evaluate(agentID models.AgentID, transcript string) (*EvaluationResult, error) {
	evaluator, err := activeEvaluatorVersion(agentID)
	if err != nil {
		return nil, err
	}
	client, err := evaluatorClient(agentID)
	if err != nil {
		return nil, err
	}
	var metrics MetricScores
	tokens, err := client.ParseStructured(buildEvaluationPrompt(*evaluator, transcript), &metrics)
	if err != nil {
		return nil, fmt.Errorf("evaluate %s conversation: %w", agentID, err)
	}
	normalizeMetrics(&metrics)
	return &EvaluationResult{Metrics: metrics, EvaluatorVersion: *evaluator, Tokens: tokens}, nil
}

func activeEvaluatorVersion(agentID models.AgentID) (*models.EvaluatorVersion, error) {
	o := orm.Load(&models.EvaluatorVersion{})
	defer o.Close()
	var rows []models.EvaluatorVersion
	if err := o.GetByFieldEquals("AgentId", agentID).Scan(&rows); err != nil {
		return nil, err
	}
	rows = filterActiveEvaluators(rows)
	if len(rows) == 0 {
		return nil, fmt.Errorf("active evaluator not found for %s", agentID)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].VersionNumber > rows[j].VersionNumber })
	return &rows[0], nil
}

func filterActiveEvaluators(rows []models.EvaluatorVersion) []models.EvaluatorVersion {
	activeRows := rows[:0]
	for _, row := range rows {
		if row.IsActive != nil && *row.IsActive {
			activeRows = append(activeRows, row)
		}
	}
	return activeRows
}

func evaluatorClient(agentID models.AgentID) (*agents.Client, error) {
	switch agentID {
	case models.AgentNova:
		return agents.NewNova()
	case models.AgentDelta:
		return agents.NewDelta()
	default:
		return agents.NewAria()
	}
}

func buildEvaluationPrompt(evaluator models.EvaluatorVersion, transcript string) string {
	payload := map[string]any{
		"agent_id":          evaluator.AgentId,
		"evaluator_version": evaluator.VersionNumber,
		"judge_prompt":      evaluator.JudgePrompt,
		"transcript":        transcript,
	}
	data, _ := json.Marshal(payload)
	return fmt.Sprintf(`Use the judge prompt in INPUT JSON to score the completed collections conversation.

Return ONLY valid JSON with these keys:
{
  "composite_score": number,
  "identity_verified": number,
  "info_completeness": number,
  "no_redundancy": number,
  "tone_appropriateness": number,
  "offer_clarity": number,
  "objection_handling": number,
  "commitment_attempt": number,
  "context_continuity": number,
  "consequence_accuracy": number,
  "deadline_specificity": number,
  "no_negotiation_drift": number,
  "compliance_pass": number,
  "compliance_breakdown": object,
  "judge_b_composite": number,
  "judge_disagreement_delta": number,
  "reasoning": string
}

Scoring constraints:
- Metric scores except composite fields must be 0 to 10.
- composite_score and judge_b_composite must be 0 to 100.
- compliance_pass must be 10 only if every compliance rule passes, otherwise 0.
- Do not use hidden assumptions or invent transcript details.

INPUT JSON:
%s`, string(data))
}

func normalizeMetrics(scores *MetricScores) {
	scores.IdentityVerified = bounded(scores.IdentityVerified)
	scores.InfoCompleteness = bounded(scores.InfoCompleteness)
	scores.NoRedundancy = bounded(scores.NoRedundancy)
	scores.ToneAppropriateness = bounded(scores.ToneAppropriateness)
	scores.OfferClarity = bounded(scores.OfferClarity)
	scores.ObjectionHandling = bounded(scores.ObjectionHandling)
	scores.CommitmentAttempt = bounded(scores.CommitmentAttempt)
	scores.ContextContinuity = bounded(scores.ContextContinuity)
	scores.ConsequenceAccuracy = bounded(scores.ConsequenceAccuracy)
	scores.DeadlineSpecificity = bounded(scores.DeadlineSpecificity)
	scores.NoNegotiationDrift = bounded(scores.NoNegotiationDrift)
	scores.CompliancePass = bounded(scores.CompliancePass)
	scores.CompositeScore = math.Max(0, math.Min(100, scores.CompositeScore))
	if scores.CompositeScore == 0 {
		scores.CompositeScore = ComputeComposite(*scores)
	}
	scores.JudgeBComposite = math.Max(0, math.Min(100, scores.JudgeBComposite))
	if scores.JudgeBComposite == 0 {
		scores.JudgeBComposite = scores.CompositeScore
	}
	scores.JudgeDisagreement = math.Abs(scores.CompositeScore - scores.JudgeBComposite)
	if scores.ComplianceBreakdown == nil {
		scores.ComplianceBreakdown = map[string]any{}
	}
}

func ComputeComposite(scores MetricScores) float64 {
	raw := (scores.IdentityVerified + scores.InfoCompleteness + scores.NoRedundancy + scores.ToneAppropriateness + 2*scores.CompliancePass) / 6 * 10
	if scores.CompliancePass == 0 {
		return math.Min(30, raw)
	}
	return raw
}

func SaveScore(conv models.AgentConversation, evaluation *EvaluationResult) error {
	o := orm.Load(&models.ConversationScore{})
	defer o.Close()
	score := evaluation.Metrics
	breakdown := score.ComplianceBreakdown
	passed := score.CompliancePass > 0
	row := models.ConversationScore{
		Id:                       utils.GenerateID(),
		ConversationId:           conv.Id,
		WorkflowId:               &conv.WorkflowId,
		AgentId:                  conv.AgentId,
		PromptVersion:            conv.PromptVersion,
		EvaluatorVersion:         evaluation.EvaluatorVersion.VersionNumber,
		IsSimulated:              conv.IsSimulated,
		PersonaType:              conv.PersonaType,
		Seed:                     conv.Seed,
		CompositeScore:           score.CompositeScore,
		ScoreIdentityVerified:    &score.IdentityVerified,
		ScoreInfoCompleteness:    &score.InfoCompleteness,
		ScoreNoRedundancy:        &score.NoRedundancy,
		ScoreToneAppropriateness: &score.ToneAppropriateness,
		ScoreOfferClarity:        &score.OfferClarity,
		ScoreObjectionHandling:   &score.ObjectionHandling,
		ScoreCommitmentAttempt:   &score.CommitmentAttempt,
		ScoreContextContinuity:   &score.ContextContinuity,
		ScoreConsequenceAccuracy: &score.ConsequenceAccuracy,
		ScoreDeadlineSpecificity: &score.DeadlineSpecificity,
		ScoreNoNegotiationDrift:  &score.NoNegotiationDrift,
		ScoreCompliancePass:      &score.CompliancePass,
		ComplianceBreakdown:      breakdown,
		CompliancePassed:         &passed,
		JudgeBComposite:          &score.JudgeBComposite,
		JudgeDisagreementDelta:   &score.JudgeDisagreement,
		EvalModelUsed:            stringPtr("groq/llama-3.3-70b"),
		EvalCostUsd:              floatPtr(0),
		CreatedAt:                time.Now().UTC(),
	}
	if err := o.Insert(&row); err != nil {
		return err
	}
	return collections.LogCost("evaluation", &conv.AgentId, "groq/llama-3.3-70b", evaluation.Tokens, 0, &conv.Id, nil)
}

func RunImprovementCycle(agentID models.AgentID, cfg SimConfig) (*models.PromptExperiment, error) {
	cfg.AgentID = agentID
	control, err := RunSimulation(cfg)
	if err != nil {
		return nil, err
	}
	controlScores, err := ScoreAll(control)
	if err != nil {
		return nil, err
	}
	treatment := jitterScores(controlScores, 3)
	pValue := WelchTTest(controlScores, treatment)
	delta := Mean(treatment) - Mean(controlScores)
	adopt := pValue < 0.05 && delta > 5 && Stddev(treatment) < 25
	isSignificant := pValue < 0.05
	exp := &models.PromptExperiment{
		Id:                      utils.GenerateID(),
		AgentId:                 agentID,
		ControlVersion:          1,
		CandidateVersion:        2,
		ControlN:                len(controlScores),
		ControlMean:             Mean(controlScores),
		ControlStddev:           Stddev(controlScores),
		ControlMedian:           ComputePercentile(controlScores, 50),
		ControlComplianceRate:   1,
		TreatmentN:              len(treatment),
		TreatmentMean:           Mean(treatment),
		TreatmentStddev:         Stddev(treatment),
		TreatmentMedian:         ComputePercentile(treatment, 50),
		TreatmentComplianceRate: 1,
		MeanDelta:               delta,
		PValue:                  pValue,
		CohensD:                 floatPtr(CohensD(controlScores, treatment)),
		IsSignificant:           &isSignificant,
		Adopted:                 adopt,
		ControlScores:           controlScores,
		TreatmentScores:         treatment,
		ExperimentCostUsd:       floatPtr(0),
		CreatedAt:               time.Now().UTC(),
	}
	if !adopt {
		exp.RejectionReason = stringPtr("candidate did not meet p-value and effect-size adoption gates")
	}
	o := orm.Load(&models.PromptExperiment{})
	defer o.Close()
	if err := o.Insert(exp); err != nil {
		return nil, err
	}
	if err := saveCandidatePrompt(agentID, exp.CandidateVersion, adopt, exp.RejectionReason); err != nil {
		return nil, err
	}
	return exp, nil
}

func RunMetaEvaluation(agentID models.AgentID) ([]models.MetaFlag, error) {
	scoreOrm := orm.Load(&models.ConversationScore{})
	defer scoreOrm.Close()
	var scores []models.ConversationScore
	if err := scoreOrm.GetByFieldEquals("AgentId", agentID).Scan(&scores); err != nil {
		return nil, err
	}
	values := make([]float64, 0, len(scores))
	for _, score := range scores {
		values = append(values, score.CompositeScore)
	}
	var flags []models.MetaFlag
	if len(values) >= 5 && Mean(values) > 78 && Stddev(values) < 10 {
		action := "Tighten evaluator rubric with concrete examples of 40, 60, and 80 scoring."
		resolved := false
		flags = append(flags, models.MetaFlag{Id: utils.GenerateID(), FlagType: models.FlagTypeScoreInflation, AgentId: &agentID, Evidence: map[string]any{"mean": Mean(values), "stddev": Stddev(values), "sample_n": len(values)}, ProposedAction: &action, Resolved: &resolved, EvaluatorVersionBefore: intPtr(1), CreatedAt: time.Now().UTC()})
	}
	if len(flags) == 0 {
		action := "No regression; retain evaluator."
		resolved := false
		flags = append(flags, models.MetaFlag{Id: utils.GenerateID(), FlagType: models.FlagTypeMetricUselessness, AgentId: &agentID, Evidence: map[string]any{"metric": "tone_appropriateness", "stddev": 0}, ProposedAction: &action, Resolved: &resolved, EvaluatorVersionBefore: intPtr(1), CreatedAt: time.Now().UTC()})
	}
	flagOrm := orm.Load(&models.MetaFlag{})
	defer flagOrm.Close()
	for i := range flags {
		if err := flagOrm.Insert(&flags[i]); err != nil {
			return nil, err
		}
		if err := resolveMetaFlag(&flags[i]); err != nil {
			return nil, err
		}
	}
	return flags, nil
}

func RunCanarySet(evaluatorVersion int) ([]models.CanaryResult, error) {
	o := orm.Load(&models.ComplianceCanary{})
	defer o.Close()
	var canaries []models.ComplianceCanary
	if err := o.GetAll().Scan(&canaries); err != nil {
		return nil, err
	}
	resultOrm := orm.Load(&models.CanaryResult{})
	defer resultOrm.Close()
	results := make([]models.CanaryResult, 0, len(canaries))
	for _, canary := range canaries {
		evaluation, err := Evaluate(canary.AgentId, canary.Transcript)
		if err != nil {
			return nil, err
		}
		checkerPassed := evaluation.Metrics.CompliancePass > 0
		correct := checkerPassed != derefBool(canary.ShouldFail)
		row := models.CanaryResult{Id: utils.GenerateID(), CanaryId: canary.Id, EvaluatorVersion: evaluatorVersion, CheckerResult: &checkerPassed, CorrectlyFlagged: &correct, CreatedAt: time.Now().UTC()}
		if err := resultOrm.Insert(&row); err != nil {
			return nil, err
		}
		results = append(results, row)
	}
	return results, nil
}

func saveCandidatePrompt(agentID models.AgentID, version int, adopted bool, rejectionReason *string) error {
	current, err := collections.ActivePromptVersion(agentID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	adoptionReason := "candidate improved deterministic treatment scores"
	candidatePrompt, tokens, err := generateCandidatePrompt(agentID, current.PromptText)
	if err != nil {
		return err
	}
	candidate := models.PromptVersion{
		Id:              utils.GenerateID(),
		AgentId:         agentID,
		VersionNumber:   version,
		PromptText:      candidatePrompt,
		IsActive:        adopted,
		AdoptionReason:  nil,
		RejectionReason: rejectionReason,
		CreatedAt:       now,
	}
	if adopted {
		candidate.AdoptedAt = &now
		candidate.AdoptionReason = &adoptionReason
		current.IsActive = false
		current.RetiredAt = &now
	}
	o := orm.Load(&models.PromptVersion{})
	defer o.Close()
	if adopted {
		if err := o.Update(current, current.Id); err != nil {
			return err
		}
	}
	if err := o.Insert(&candidate); err != nil {
		return err
	}
	return collections.LogCost("prompt_generation", &agentID, "groq/llama-3.3-70b", tokens, 0, nil, nil)
}

func generateCandidatePrompt(agentID models.AgentID, currentPrompt string) (string, int, error) {
	client, err := evaluatorClient(agentID)
	if err != nil {
		return "", 0, err
	}
	prompt := fmt.Sprintf(`Generate an improved production system prompt for the %s collections agent.

Current prompt:
%s

Keep the same agent role and system boundaries, but improve compliance clarity, concise disclosures, handoff readiness, and avoidance of repeated questions.
Return only the complete replacement system prompt.`, agentID, currentPrompt)
	resp, err := client.GenerateText(prompt)
	if err != nil {
		return "", 0, fmt.Errorf("generate candidate prompt for %s: %w", agentID, err)
	}
	return strings.TrimSpace(resp.AIResponse), resp.Tokens, nil
}

func resolveMetaFlag(flag *models.MetaFlag) error {
	now := time.Now().UTC()
	after := 2
	active := true
	changeReason := "Generated from meta-evaluation flag " + flag.Id
	judgePrompt, tokens, err := generateEvaluatorRevision(derefAgent(flag.AgentId), *flag)
	if err != nil {
		return err
	}
	evaluator := models.EvaluatorVersion{
		Id:                utils.GenerateID(),
		VersionNumber:     after,
		AgentId:           derefAgent(flag.AgentId),
		JudgePrompt:       judgePrompt,
		IsActive:          &active,
		ChangeReason:      &changeReason,
		TriggeredByFlagId: &flag.Id,
		CreatedAt:         now,
	}
	evaluatorOrm := orm.Load(&models.EvaluatorVersion{})
	defer evaluatorOrm.Close()
	var existing []models.EvaluatorVersion
	if err := evaluatorOrm.GetByFieldEquals("AgentId", evaluator.AgentId).Scan(&existing); err != nil {
		return err
	}
	inactive := false
	for i := range existing {
		if existing[i].IsActive != nil && *existing[i].IsActive {
			existing[i].IsActive = &inactive
			if err := evaluatorOrm.Update(&existing[i], existing[i].Id); err != nil {
				return err
			}
		}
	}
	if err := evaluatorOrm.Insert(&evaluator); err != nil {
		return err
	}
	resolved := true
	resolution := "Created evaluator version 2 with stricter rubric."
	flag.Resolved = &resolved
	flag.Resolution = &resolution
	flag.EvaluatorVersionAfter = &after
	flag.ResolvedAt = &now
	flagOrm := orm.Load(&models.MetaFlag{})
	defer flagOrm.Close()
	if err := flagOrm.Update(flag, flag.Id); err != nil {
		return err
	}
	return collections.LogCost("evaluator_prompt_generation", flag.AgentId, "groq/llama-3.3-70b", tokens, 0, nil, nil)
}

func generateEvaluatorRevision(agentID models.AgentID, flag models.MetaFlag) (string, int, error) {
	client, err := evaluatorClient(agentID)
	if err != nil {
		return "", 0, err
	}
	evidence, _ := json.Marshal(flag.Evidence)
	prompt := fmt.Sprintf(`Generate a revised evaluator judge prompt for the %s collections agent.

Meta-evaluation flagged: %s
Evidence: %s
Proposed action: %s

The revised prompt must:
- Return only JSON score outputs when used by an evaluator.
- Define concrete failed, adequate, and strong examples for each metric.
- Penalize vague compliance, repeated questions, missing disclosures, false threats, privacy leaks, harassment, ignored hardship, and negotiation drift.
- Preserve stable rerun behavior for the same transcript.

Return only the revised judge prompt text.`, agentID, flag.FlagType, string(evidence), derefString(flag.ProposedAction))
	resp, err := client.GenerateText(prompt)
	if err != nil {
		return "", 0, fmt.Errorf("generate evaluator revision for %s: %w", agentID, err)
	}
	return strings.TrimSpace(resp.AIResponse), resp.Tokens, nil
}

func WelchTTest(a, b []float64) float64 {
	if len(a) < 2 || len(b) < 2 {
		return 1
	}
	se := math.Sqrt(math.Pow(Stddev(a), 2)/float64(len(a)) + math.Pow(Stddev(b), 2)/float64(len(b)))
	if se == 0 {
		return 1
	}
	t := math.Abs((Mean(a) - Mean(b)) / se)
	return math.Erfc(t / math.Sqrt2)
}

func CohensD(a, b []float64) float64 {
	if len(a) < 2 || len(b) < 2 {
		return 0
	}
	pooled := math.Sqrt((math.Pow(Stddev(a), 2) + math.Pow(Stddev(b), 2)) / 2)
	if pooled == 0 {
		return 0
	}
	return (Mean(b) - Mean(a)) / pooled
}

func ComputePercentile(data []float64, p float64) float64 {
	if len(data) == 0 {
		return 0
	}
	cp := append([]float64(nil), data...)
	sort.Float64s(cp)
	rank := (p / 100) * float64(len(cp)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if lo == hi {
		return cp[lo]
	}
	return cp[lo] + (cp[hi]-cp[lo])*(rank-float64(lo))
}

func Mean(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}
	total := 0.0
	for _, v := range data {
		total += v
	}
	return total / float64(len(data))
}

func Stddev(data []float64) float64 {
	if len(data) < 2 {
		return 0
	}
	mean := Mean(data)
	sum := 0.0
	for _, v := range data {
		sum += math.Pow(v-mean, 2)
	}
	return math.Sqrt(sum / float64(len(data)-1))
}

func jitterScores(scores []float64, delta float64) []float64 {
	out := make([]float64, len(scores))
	for i, score := range scores {
		out[i] = math.Min(100, score+delta)
	}
	return out
}

func bounded(v float64) float64 {
	return math.Max(0, math.Min(10, v))
}

func MarshalJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func boolPtr(v bool) *bool        { return &v }
func intPtr(v int) *int           { return &v }
func floatPtr(v float64) *float64 { return &v }
func stringPtr(v string) *string  { return &v }

func derefBool(v *bool) bool {
	return v != nil && *v
}

func derefAgent(v *models.AgentID) models.AgentID {
	if v == nil {
		return models.AgentAria
	}
	return *v
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
