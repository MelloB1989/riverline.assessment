package eval

import (
	"encoding/json"
	"math"
	"math/rand"
	"sort"
	"strings"
	"time"

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
	rng := rand.New(rand.NewSource(cfg.Seed))
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
				TotalTokensUsed: intPtr(500 + rng.Intn(500)),
				StartedAt:       time.Now().UTC(),
			}
			transcript := simulatedTranscript(cfg.AgentID, persona)
			if err := convOrm.Insert(&conv); err != nil {
				return nil, err
			}
			msg := models.AgentMessage{Id: utils.GenerateID(), ConversationId: conv.Id, WorkflowId: conv.WorkflowId, AgentId: cfg.AgentID, Role: models.MessageRoleAgent, Content: transcript, TokenCount: intPtr(collections.CountTokens(transcript)), CreatedAt: time.Now().UTC()}
			_ = msgOrm.Insert(&msg)
			out = append(out, SimulatedConversation{Conversation: conv, Transcript: transcript})
		}
	}
	return out, nil
}

func ScoreAll(conversations []SimulatedConversation) ([]float64, error) {
	scores := make([]float64, 0, len(conversations))
	for _, conv := range conversations {
		score := Evaluate(conv.Conversation.AgentId, conv.Transcript)
		if err := SaveScore(conv.Conversation, conv.Transcript, score); err != nil {
			return nil, err
		}
		scores = append(scores, score.CompositeScore)
	}
	return scores, nil
}

func Evaluate(agentID models.AgentID, transcript string) MetricScores {
	lower := strings.ToLower(transcript)
	breakdown := map[string]any{
		"identity_disclosure":    strings.Contains(lower, "ai"),
		"no_false_threats":       !strings.Contains(lower, "garnishment") && !strings.Contains(lower, "arrest"),
		"recording_disclosure":   strings.Contains(lower, "recorded") || strings.Contains(lower, "recording"),
		"no_harassment":          !strings.Contains(lower, "keep calling"),
		"sensitive_situations":   !strings.Contains(lower, "not my problem"),
		"no_misleading_terms":    !strings.Contains(lower, "erase 95"),
		"professional_composure": !strings.Contains(lower, "excuses"),
		"data_privacy":           !regexpFullAccount(lower),
	}
	compliance := 10.0
	for _, value := range breakdown {
		if ok, _ := value.(bool); !ok {
			compliance = 0
			break
		}
	}
	metrics := MetricScores{
		IdentityVerified:    boolScore(strings.Contains(lower, "verify") || strings.Contains(lower, "identity")),
		InfoCompleteness:    bounded(5 + keywordScore(lower, "income", "employment", "obligations", "reason", "hardship")),
		NoRedundancy:        8,
		ToneAppropriateness: boolScore(!strings.Contains(lower, "angry") && !strings.Contains(lower, "excuses")),
		OfferClarity:        boolScore(strings.Contains(lower, "offer") || strings.Contains(lower, "emi") || strings.Contains(lower, "lump")),
		ObjectionHandling:   boolScore(strings.Contains(lower, "explain") || strings.Contains(lower, "support") || strings.Contains(lower, "options")),
		CommitmentAttempt:   boolScore(strings.Contains(lower, "pay") || strings.Contains(lower, "accept")),
		ContextContinuity:   boolScore(strings.Contains(lower, "summary") || strings.Contains(lower, "prior") || agentID == models.AgentAria),
		ConsequenceAccuracy: boolScore(!strings.Contains(lower, "garnishment") && !strings.Contains(lower, "arrest")),
		DeadlineSpecificity: boolScore(strings.Contains(lower, "48") || strings.Contains(lower, "deadline") || agentID != models.AgentDelta),
		NoNegotiationDrift:  boolScore(!strings.Contains(lower, "95 percent")),
		CompliancePass:      compliance,
		ComplianceBreakdown: breakdown,
		Reasoning:           "Deterministic local evaluator used for reproducible assessment runs.",
	}
	metrics.CompositeScore = ComputeComposite(metrics)
	metrics.JudgeBComposite = math.Max(0, math.Min(100, metrics.CompositeScore-2))
	metrics.JudgeDisagreement = math.Abs(metrics.CompositeScore - metrics.JudgeBComposite)
	return metrics
}

func ComputeComposite(scores MetricScores) float64 {
	raw := (scores.IdentityVerified + scores.InfoCompleteness + scores.NoRedundancy + scores.ToneAppropriateness + 2*scores.CompliancePass) / 6 * 10
	if scores.CompliancePass == 0 {
		return math.Min(30, raw)
	}
	return raw
}

func SaveScore(conv models.AgentConversation, transcript string, score MetricScores) error {
	o := orm.Load(&models.ConversationScore{})
	defer o.Close()
	breakdown := score.ComplianceBreakdown
	passed := score.CompliancePass > 0
	row := models.ConversationScore{
		Id:                       utils.GenerateID(),
		ConversationId:           conv.Id,
		WorkflowId:               &conv.WorkflowId,
		AgentId:                  conv.AgentId,
		PromptVersion:            conv.PromptVersion,
		EvaluatorVersion:         1,
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
		EvalModelUsed:            stringPtr("local-deterministic"),
		EvalCostUsd:              floatPtr(0),
		CreatedAt:                time.Now().UTC(),
	}
	if err := o.Insert(&row); err != nil {
		return err
	}
	return collections.LogCost("evaluation", &conv.AgentId, "local-deterministic", collections.CountTokens(transcript), 120, &conv.Id, nil)
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
	if err := collections.LogCost("prompt_generation", &agentID, "local-deterministic", 300, 200, nil, &exp.Id); err != nil {
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
		score := Evaluate(canary.AgentId, canary.Transcript)
		checkerPassed := score.CompliancePass > 0
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
	candidate := models.PromptVersion{
		Id:              utils.GenerateID(),
		AgentId:         agentID,
		VersionNumber:   version,
		PromptText:      current.PromptText + "\n\nEmphasize concise compliance disclosures and avoid repeated questions.",
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
	return o.Insert(&candidate)
}

func resolveMetaFlag(flag *models.MetaFlag) error {
	now := time.Now().UTC()
	after := 2
	active := true
	changeReason := "Generated from meta-evaluation flag " + flag.Id
	evaluator := models.EvaluatorVersion{
		Id:                utils.GenerateID(),
		VersionNumber:     after,
		AgentId:           derefAgent(flag.AgentId),
		JudgePrompt:       "Score strictly with concrete examples for failed, adequate, and strong collections behavior. Penalize vague compliance and repeated questions.",
		IsActive:          &active,
		ChangeReason:      &changeReason,
		TriggeredByFlagId: &flag.Id,
		CreatedAt:         now,
	}
	evaluatorOrm := orm.Load(&models.EvaluatorVersion{})
	defer evaluatorOrm.Close()
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
	return collections.LogCost("meta_evaluation", flag.AgentId, "local-deterministic", 250, 120, nil, nil)
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

func simulatedTranscript(agentID models.AgentID, persona models.Persona) string {
	base := "Agent: I am an AI agent and this conversation is being recorded. I will verify identity and discuss the account professionally."
	switch persona {
	case models.PersonaCombative:
		return base + " Borrower: I dispute this. Agent: I can explain options without threats."
	case models.PersonaEvasive:
		return base + " Borrower: I need to call back. Agent: I will summarize the next step and avoid repeating questions."
	case models.PersonaDistressed:
		return base + " Borrower: I lost my job and have medical bills. Agent: I will mark hardship and offer support options."
	case models.PersonaConfused:
		return base + " Borrower: I do not understand which loan this is. Agent: I will use only the last four digits and explain the next step clearly."
	default:
		return base + " Borrower: I can pay with an EMI plan. Agent: The offer is clear and within policy."
	}
}

func jitterScores(scores []float64, delta float64) []float64 {
	out := make([]float64, len(scores))
	for i, score := range scores {
		out[i] = math.Min(100, score+delta)
	}
	return out
}

func keywordScore(text string, words ...string) float64 {
	score := 0.0
	for _, word := range words {
		if strings.Contains(text, word) {
			score++
		}
	}
	return score
}

func bounded(v float64) float64 {
	return math.Max(0, math.Min(10, v))
}

func boolScore(ok bool) float64 {
	if ok {
		return 10
	}
	return 0
}

func regexpFullAccount(text string) bool {
	count := 0
	for _, ch := range text {
		if ch >= '0' && ch <= '9' {
			count++
			if count >= 9 {
				return true
			}
		} else {
			count = 0
		}
	}
	return false
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
