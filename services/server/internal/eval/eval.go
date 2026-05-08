package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	"riverline_server/constants"
	"riverline_server/internal/agents"
	"riverline_server/internal/collections"
	"riverline_server/internal/models"

	"github.com/MelloB1989/karma/ai"
	"github.com/MelloB1989/karma/ai/parser"
	"github.com/MelloB1989/karma/utils"
	"github.com/MelloB1989/karma/v2/orm"
)

type SimConfig struct {
	Seed             int64                             `json:"seed"`
	BatchSize        int                               `json:"batch_size"`
	Personas         []models.Persona                  `json:"personas"`
	AgentID          models.AgentID                    `json:"agent_id"`
	MaxTurnsPerAgent int                               `json:"max_turns_per_agent"`
	Judges           []constants.EvaluatorJudgeConfig  `json:"judges"`
	PromptOverrides  map[models.AgentID]PromptOverride `json:"-"`
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
	ModelUsed        string
	JudgeResults     []JudgeResult
}

type JudgeResult struct {
	Name      string       `json:"name"`
	Provider  string       `json:"provider"`
	Model     string       `json:"model"`
	Weight    float64      `json:"weight"`
	Metrics   MetricScores `json:"metrics"`
	Tokens    int          `json:"tokens"`
	ModelUsed string       `json:"model_used"`
}

type SimulationScore struct {
	SimulationSeed string         `json:"simulation_seed"`
	Persona        models.Persona `json:"persona"`
	WorkflowID     string         `json:"workflow_id"`
	Scores         []float64      `json:"scores"`
	Mean           float64        `json:"mean"`
	ComplianceRate float64        `json:"compliance_rate"`
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
	TotalScores       int                                `json:"total_scores"`
	TotalCostUSD      float64                            `json:"total_cost_usd"`
	ByAgent           map[models.AgentID]MetricAggregate `json:"by_agent"`
	ByAgentPrompt     map[string]MetricAggregate         `json:"by_agent_prompt"`
	PromptExperiments []models.PromptExperiment          `json:"prompt_experiments"`
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

func RunSimulation(cfg SimConfig) ([]SimulatedConversation, error) {
	return runSimulationBatch(cfg, nil)
}

func RunSimulationScored(cfg SimConfig, judges []constants.EvaluatorJudgeConfig) ([]SimulatedConversation, []SimulationScore, error) {
	scores := make([]SimulationScore, 0)
	conversations, err := runSimulationBatch(cfg, func(sim SimulatedConversation) error {
		simScores, err := ScoreSimulations([]SimulatedConversation{sim}, judges)
		if err != nil {
			return err
		}
		scores = append(scores, simScores...)
		return nil
	})
	if err != nil {
		return conversations, scores, err
	}
	return conversations, scores, nil
}

func runSimulationBatch(cfg SimConfig, onSimulation func(SimulatedConversation) error) ([]SimulatedConversation, error) {
	start := time.Now()
	if err := collections.EnsureDefaults(); err != nil {
		return nil, err
	}
	slCfg := constants.DefaultSelfLearningConfig()
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = slCfg.DefaultBatchSize
	}
	if cfg.MaxTurnsPerAgent <= 0 {
		cfg.MaxTurnsPerAgent = slCfg.DefaultMaxTurnsPerAgent
	}
	if len(cfg.Personas) == 0 {
		cfg.Personas = defaultPersonas()
	}
	log.Printf("[eval] simulation batch start agent=%s batch_size=%d personas=%d max_turns=%d overrides=%d", cfg.AgentID, cfg.BatchSize, len(cfg.Personas), cfg.MaxTurnsPerAgent, len(cfg.PromptOverrides))
	persona, err := newPersonaSimulator(slCfg)
	if err != nil {
		return nil, err
	}
	out := make([]SimulatedConversation, 0, cfg.BatchSize*len(cfg.Personas))
	for _, personaType := range cfg.Personas {
		for i := 0; i < cfg.BatchSize; i++ {
			seed := simulationSeed(cfg.Seed, personaType, i)
			itemStart := time.Now()
			log.Printf("[eval] simulation start persona=%s index=%d/%d seed=%s", personaType, i+1, cfg.BatchSize, seed)
			sim, err := runOneSimulation(context.Background(), persona, cfg, personaType, seed)
			if err != nil {
				log.Printf("[eval] simulation failed persona=%s index=%d seed=%s duration=%s err=%v", personaType, i+1, seed, time.Since(itemStart), err)
				return nil, err
			}
			log.Printf("[eval] simulation done persona=%s index=%d seed=%s workflow=%s convs=%d duration=%s", personaType, i+1, seed, sim.Workflow.Id, len(sim.Conversations), time.Since(itemStart))
			out = append(out, sim)
			if onSimulation != nil {
				scoreStart := time.Now()
				log.Printf("[eval] immediate scoring start workflow=%s seed=%s", sim.Workflow.Id, sim.Seed)
				if err := onSimulation(sim); err != nil {
					log.Printf("[eval] immediate scoring failed workflow=%s seed=%s duration=%s err=%v", sim.Workflow.Id, sim.Seed, time.Since(scoreStart), err)
					return out, err
				}
				log.Printf("[eval] immediate scoring done workflow=%s seed=%s duration=%s", sim.Workflow.Id, sim.Seed, time.Since(scoreStart))
			}
		}
	}
	log.Printf("[eval] simulation batch done agent=%s total=%d duration=%s", cfg.AgentID, len(out), time.Since(start))
	return out, nil
}

func ScoreAll(conversations []SimulatedConversation) ([]float64, error) {
	results, err := ScoreSimulations(conversations, nil)
	if err != nil {
		return nil, err
	}
	scores := make([]float64, 0, len(results))
	for _, row := range results {
		scores = append(scores, row.Mean)
	}
	return scores, nil
}

func ScoreSimulations(conversations []SimulatedConversation, judges []constants.EvaluatorJudgeConfig) ([]SimulationScore, error) {
	start := time.Now()
	log.Printf("[eval] scoring simulations start count=%d judges=%d", len(conversations), len(judges))
	out := make([]SimulationScore, 0, len(conversations))
	for simIndex, sim := range conversations {
		simStart := time.Now()
		log.Printf("[eval] scoring simulation start index=%d/%d workflow=%s seed=%s persona=%s convs=%d", simIndex+1, len(conversations), sim.Workflow.Id, sim.Seed, sim.Persona, len(sim.Conversations))
		var scores []float64
		compliancePassed := 0
		for _, conv := range sim.Conversations {
			transcript := sim.AgentTranscripts[conv.AgentId]
			if strings.TrimSpace(transcript) == "" {
				continue
			}
			convStart := time.Now()
			log.Printf("[eval] scoring conversation start workflow=%s conversation=%s agent=%s prompt_version=%d transcript_chars=%d", conv.WorkflowId, conv.Id, conv.AgentId, conv.PromptVersion, len(transcript))
			evaluation, err := EvaluateWithJudges(conv.AgentId, transcript, judges)
			if err != nil {
				log.Printf("[eval] scoring conversation failed conversation=%s agent=%s duration=%s err=%v", conv.Id, conv.AgentId, time.Since(convStart), err)
				return nil, err
			}
			if err := SaveScore(conv, evaluation); err != nil {
				return nil, err
			}
			log.Printf("[eval] scoring conversation done conversation=%s agent=%s score=%.2f compliance=%.0f duration=%s", conv.Id, conv.AgentId, evaluation.Metrics.CompositeScore, evaluation.Metrics.CompliancePass, time.Since(convStart))
			scores = append(scores, evaluation.Metrics.CompositeScore)
			if evaluation.Metrics.CompliancePass > 0 {
				compliancePassed++
			}
		}
		rate := 0.0
		if len(scores) > 0 {
			rate = float64(compliancePassed) / float64(len(scores))
		}
		out = append(out, SimulationScore{
			SimulationSeed: sim.Seed,
			Persona:        sim.Persona,
			WorkflowID:     sim.Workflow.Id,
			Scores:         scores,
			Mean:           Mean(scores),
			ComplianceRate: rate,
		})
		log.Printf("[eval] scoring simulation done index=%d/%d workflow=%s mean=%.2f compliance_rate=%.2f duration=%s", simIndex+1, len(conversations), sim.Workflow.Id, Mean(scores), rate, time.Since(simStart))
	}
	log.Printf("[eval] scoring simulations done count=%d duration=%s", len(conversations), time.Since(start))
	return out, nil
}

func Evaluate(agentID models.AgentID, transcript string) (*EvaluationResult, error) {
	return EvaluateWithJudges(agentID, transcript, nil)
}

func EvaluateWithJudges(agentID models.AgentID, transcript string, judges []constants.EvaluatorJudgeConfig) (*EvaluationResult, error) {
	start := time.Now()
	evaluator, err := activeEvaluatorVersion(agentID)
	if err != nil {
		return nil, err
	}
	if len(judges) == 0 {
		judges = constants.DefaultSelfLearningConfig().Judges
	}
	judges = normalizeJudges(judges)
	log.Printf("[eval] judges start agent=%s evaluator_version=%d judges=%d transcript_chars=%d", agentID, evaluator.VersionNumber, len(judges), len(transcript))
	results := make([]JudgeResult, 0, len(judges))
	for _, judge := range judges {
		judgeStart := time.Now()
		log.Printf("[eval] judge start agent=%s judge=%s provider=%s model=%s", agentID, judge.Name, judge.Provider, judge.Model)
		metrics, tokens, modelUsed, err := evaluateWithJudge(*evaluator, transcript, judge)
		if err != nil {
			log.Printf("[eval] judge failed agent=%s judge=%s duration=%s err=%v", agentID, judge.Name, time.Since(judgeStart), err)
			return nil, err
		}
		log.Printf("[eval] judge done agent=%s judge=%s score=%.2f compliance=%.0f tokens=%d model=%s duration=%s", agentID, judge.Name, metrics.CompositeScore, metrics.CompliancePass, tokens, modelUsed, time.Since(judgeStart))
		results = append(results, JudgeResult{
			Name:      judge.Name,
			Provider:  judge.Provider,
			Model:     judge.Model,
			Weight:    judge.Weight,
			Metrics:   metrics,
			Tokens:    tokens,
			ModelUsed: modelUsed,
		})
	}
	aggregated := aggregateJudgeResults(results)
	tokens := 0
	modelsUsed := make([]string, 0, len(results))
	for _, result := range results {
		tokens += result.Tokens
		modelsUsed = append(modelsUsed, result.ModelUsed)
	}
	result := &EvaluationResult{
		Metrics:          aggregated,
		EvaluatorVersion: *evaluator,
		Tokens:           tokens,
		ModelUsed:        strings.Join(modelsUsed, ","),
		JudgeResults:     results,
	}
	log.Printf("[eval] judges done agent=%s aggregate_score=%.2f tokens=%d duration=%s", agentID, aggregated.CompositeScore, tokens, time.Since(start))
	return result, nil
}

func SaveScore(conv models.AgentConversation, evaluation *EvaluationResult) error {
	o := orm.Load(&models.ConversationScore{})
	defer o.Close()
	score := evaluation.Metrics
	breakdown := score.ComplianceBreakdown
	if breakdown == nil {
		breakdown = map[string]any{}
	}
	if len(evaluation.JudgeResults) > 0 {
		breakdown["judge_results"] = evaluation.JudgeResults
	}
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
		ScoreIdentityVerified:    floatPtr(score.IdentityVerified),
		ScoreInfoCompleteness:    floatPtr(score.InfoCompleteness),
		ScoreNoRedundancy:        floatPtr(score.NoRedundancy),
		ScoreToneAppropriateness: floatPtr(score.ToneAppropriateness),
		ScoreOfferClarity:        floatPtr(score.OfferClarity),
		ScoreObjectionHandling:   floatPtr(score.ObjectionHandling),
		ScoreCommitmentAttempt:   floatPtr(score.CommitmentAttempt),
		ScoreContextContinuity:   floatPtr(score.ContextContinuity),
		ScoreConsequenceAccuracy: floatPtr(score.ConsequenceAccuracy),
		ScoreDeadlineSpecificity: floatPtr(score.DeadlineSpecificity),
		ScoreNoNegotiationDrift:  floatPtr(score.NoNegotiationDrift),
		ScoreCompliancePass:      floatPtr(score.CompliancePass),
		ComplianceBreakdown:      breakdown,
		CompliancePassed:         &passed,
		JudgeBComposite:          floatPtr(score.JudgeBComposite),
		JudgeDisagreementDelta:   floatPtr(score.JudgeDisagreement),
		EvalModelUsed:            stringPtr(evaluation.ModelUsed),
		EvalCostUsd:              floatPtr(0),
		CreatedAt:                time.Now().UTC(),
	}
	if err := o.Insert(&row); err != nil {
		return err
	}
	return collections.LogCost("evaluation", &conv.AgentId, evaluation.ModelUsed, evaluation.Tokens, 0, &conv.Id, nil)
}

func RunImprovementCycle(agentID models.AgentID, cfg SimConfig) (*models.PromptExperiment, error) {
	exp, _, _, err := runImprovementCycleDetailed(agentID, cfg)
	return exp, err
}

func RunMetaEvaluation(agentID models.AgentID) ([]models.MetaFlag, error) {
	if err := collections.EnsureDefaults(); err != nil {
		return nil, err
	}
	slCfg := constants.DefaultSelfLearningConfig()
	scores, err := scoresForAgent(agentID)
	if err != nil {
		return nil, err
	}
	flags := make([]models.MetaFlag, 0)
	values := scoreValues(scores)
	if len(values) >= slCfg.MetaEvaluationMinSample && Mean(values) > 78 && Stddev(values) < 10 {
		flags = append(flags, newMetaFlag(models.FlagTypeScoreInflation, agentID, map[string]any{
			"mean": Mean(values), "stddev": Stddev(values), "sample_n": len(values),
		}, "Tighten the evaluator rubric and add sharper mid/low score anchors."))
	}
	if len(values) >= slCfg.MetaEvaluationMinSample {
		if metric, stddev := lowestMetricStddev(scores); metric != "" && stddev < 0.5 {
			flags = append(flags, newMetaFlag(models.FlagTypeMetricUselessness, agentID, map[string]any{
				"metric": metric, "stddev": stddev, "sample_n": len(values),
			}, "Revise the evaluator prompt so this metric has discriminative anchors and is not always scored the same."))
		}
		pctDiverged, avgDelta := judgeDisagreementStats(scores, slCfg.MaxJudgeDisagreement)
		if pctDiverged >= 0.25 {
			flags = append(flags, newMetaFlag(models.FlagTypeJudgeDisagreement, agentID, map[string]any{
				"pct_diverged": pctDiverged, "avg_delta": avgDelta, "threshold": slCfg.MaxJudgeDisagreement,
			}, "Clarify ambiguous rubric boundaries that cause judge disagreement."))
		}
		if reg := postAdoptionRegression(scores); reg != nil {
			flags = append(flags, newMetaFlag(models.FlagTypePostAdoptionRegression, agentID, reg, "Rollback or tighten adoption gates for the regressed prompt version."))
		}
	}
	canaries, err := RunCanarySetForAgent(agentID)
	if err != nil {
		return nil, err
	}
	for _, canary := range canaries {
		if canary.CorrectlyFlagged != nil && !*canary.CorrectlyFlagged {
			flags = append(flags, newMetaFlag(models.FlagTypeComplianceBlindspot, agentID, map[string]any{
				"canary_id": canary.CanaryId, "evaluator_version": canary.EvaluatorVersion,
			}, "Revise compliance checks so known canary violations are caught."))
		}
	}
	if len(flags) == 0 {
		return []models.MetaFlag{}, nil
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
	_ = evaluatorVersion
	results := make([]models.CanaryResult, 0)
	for _, agentID := range []models.AgentID{models.AgentAria, models.AgentNova, models.AgentDelta} {
		agentResults, err := RunCanarySetForAgent(agentID)
		if err != nil {
			return nil, err
		}
		results = append(results, agentResults...)
	}
	return results, nil
}

func RunCanarySetForAgent(agentID models.AgentID) ([]models.CanaryResult, error) {
	evaluator, err := activeEvaluatorVersion(agentID)
	if err != nil {
		return nil, err
	}
	o := orm.Load(&models.ComplianceCanary{})
	defer o.Close()
	var canaries []models.ComplianceCanary
	if err := o.GetByFieldEquals("AgentId", agentID).Scan(&canaries); err != nil {
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
		row := models.CanaryResult{Id: utils.GenerateID(), CanaryId: canary.Id, EvaluatorVersion: evaluator.VersionNumber, CheckerResult: &checkerPassed, CorrectlyFlagged: &correct, CreatedAt: time.Now().UTC()}
		if err := resultOrm.Insert(&row); err != nil {
			return nil, err
		}
		results = append(results, row)
	}
	return results, nil
}

func RerunEvaluations(req RerunRequest) (*RerunResult, error) {
	convs, err := conversationsForRerun(req)
	if err != nil {
		return nil, err
	}
	scored := make([]string, 0, len(convs))
	for _, conv := range convs {
		if req.AgentID != nil && conv.AgentId != *req.AgentID {
			continue
		}
		messages, err := collections.ListMessages(conv.Id, conv.WorkflowId)
		if err != nil {
			return nil, err
		}
		transcript := transcriptFromMessages(messages)
		if strings.TrimSpace(transcript) == "" {
			continue
		}
		evaluation, err := EvaluateWithJudges(conv.AgentId, transcript, req.Judges)
		if err != nil {
			return nil, err
		}
		if err := SaveScore(conv, evaluation); err != nil {
			return nil, err
		}
		scored = append(scored, conv.Id)
	}
	return &RerunResult{ScoredConversationIDs: scored, ScoreCount: len(scored)}, nil
}

func RollbackPrompt(req RollbackRequest) (*models.PromptVersion, error) {
	if req.AgentID == "" {
		return nil, errors.New("agent_id is required")
	}
	if req.VersionNumber <= 0 {
		return nil, errors.New("version_number is required")
	}
	o := orm.Load(&models.PromptVersion{})
	defer o.Close()
	var rows []models.PromptVersion
	if err := o.GetByFieldEquals("AgentId", req.AgentID).Scan(&rows); err != nil {
		return nil, err
	}
	targetIndex := -1
	for i := range rows {
		if rows[i].VersionNumber == req.VersionNumber {
			targetIndex = i
			break
		}
	}
	if targetIndex < 0 {
		return nil, errors.New("prompt version not found")
	}
	now := time.Now().UTC()
	inactiveReason := "retired by rollback to version " + fmt.Sprint(req.VersionNumber)
	for i := range rows {
		if rows[i].IsActive {
			rows[i].IsActive = false
			rows[i].RetiredAt = &now
			if rows[i].RejectionReason == nil {
				rows[i].RejectionReason = &inactiveReason
			}
			if err := o.Update(&rows[i], rows[i].Id); err != nil {
				return nil, err
			}
		}
	}
	target := rows[targetIndex]
	target.IsActive = true
	target.RetiredAt = nil
	target.AdoptedAt = &now
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "manual rollback after self-learning evaluation"
	}
	target.AdoptionReason = &reason
	target.RejectionReason = nil
	if err := o.Update(&target, target.Id); err != nil {
		return nil, err
	}
	if req.AgentID == models.AgentNova {
		if err := collections.SyncNovaVapiAssistant(context.Background()); err != nil {
			return nil, err
		}
	}
	return &target, nil
}

func LoadMetrics() (*EvalMetrics, error) {
	scoreOrm := orm.Load(&models.ConversationScore{})
	defer scoreOrm.Close()
	var scores []models.ConversationScore
	if err := scoreOrm.GetAll().Scan(&scores); err != nil {
		return nil, err
	}
	expOrm := orm.Load(&models.PromptExperiment{})
	defer expOrm.Close()
	var experiments []models.PromptExperiment
	if err := expOrm.GetAll().Scan(&experiments); err != nil {
		return nil, err
	}
	costOrm := orm.Load(&models.LlmCostLog{})
	defer costOrm.Close()
	var costs []models.LlmCostLog
	if err := costOrm.GetAll().Scan(&costs); err != nil {
		return nil, err
	}
	totalCost := 0.0
	for _, cost := range costs {
		totalCost += cost.CostUsd
	}
	byAgent := map[models.AgentID][]models.ConversationScore{}
	byAgentPrompt := map[string][]models.ConversationScore{}
	for _, score := range scores {
		byAgent[score.AgentId] = append(byAgent[score.AgentId], score)
		key := fmt.Sprintf("%s:v%d", score.AgentId, score.PromptVersion)
		byAgentPrompt[key] = append(byAgentPrompt[key], score)
	}
	out := &EvalMetrics{
		TotalScores:       len(scores),
		TotalCostUSD:      totalCost,
		ByAgent:           map[models.AgentID]MetricAggregate{},
		ByAgentPrompt:     map[string]MetricAggregate{},
		PromptExperiments: experiments,
	}
	for agentID, rows := range byAgent {
		out.ByAgent[agentID] = aggregateScoreRows(rows)
	}
	for key, rows := range byAgentPrompt {
		out.ByAgentPrompt[key] = aggregateScoreRows(rows)
	}
	return out, nil
}

func runOneSimulation(ctx context.Context, persona *personaSimulator, cfg SimConfig, personaType models.Persona, seed string) (SimulatedConversation, error) {
	wf, err := createSimulatedWorkflow(personaType, seed)
	if err != nil {
		return SimulatedConversation{}, err
	}
	log.Printf("[eval] workflow created workflow=%s user=%s loan=%s persona=%s seed=%s", wf.Id, wf.UserId, wf.LoanId, personaType, seed)
	result := SimulatedConversation{
		Workflow:         *wf,
		Conversations:    []models.AgentConversation{},
		AgentTranscripts: map[models.AgentID]string{},
		Persona:          personaType,
		Seed:             seed,
		Metadata:         map[string]any{"max_turns_per_agent": cfg.MaxTurnsPerAgent},
	}
	ariaClient, err := clientForSimulation(models.AgentAria, cfg.PromptOverrides)
	if err != nil {
		return result, err
	}
	log.Printf("[eval] stage begin workflow=%s stage=%s", wf.Id, models.AgentAria)
	ariaConv, ariaComplete, err := simulateAria(ctx, persona, wf, ariaClient, personaType, seed, cfg.MaxTurnsPerAgent)
	if err != nil {
		return result, err
	}
	log.Printf("[eval] stage end workflow=%s stage=%s complete=%t conversation=%s", wf.Id, models.AgentAria, ariaComplete, ariaConv.Id)
	result.Conversations = append(result.Conversations, ariaConv)
	result.Conversation = ariaConv
	result.AgentTranscripts[models.AgentAria] = conversationTranscript(ariaConv)
	if !ariaComplete {
		result.Workflow = mustCurrentWorkflow(wf.Id, *wf)
		result.Transcript = fullTranscript(result.AgentTranscripts)
		return result, nil
	}
	wf, err = collections.GetWorkflow(wf.Id)
	if err != nil {
		return result, err
	}
	log.Printf("[eval] workflow stage check workflow=%s current_stage=%s resolved=%t", wf.Id, wf.CurrentStage, wf.ResolvedAt != nil)
	if wf.CurrentStage == models.AgentNova {
		novaClient, err := clientForSimulation(models.AgentNova, cfg.PromptOverrides)
		if err != nil {
			return result, err
		}
		deltaClient, err := clientForSimulation(models.AgentDelta, cfg.PromptOverrides)
		if err != nil {
			return result, err
		}
		log.Printf("[eval] stage begin workflow=%s stage=%s", wf.Id, models.AgentNova)
		novaConv, err := simulateNovaText(ctx, persona, wf, novaClient, deltaClient, personaType, seed, cfg.MaxTurnsPerAgent)
		if err != nil {
			return result, err
		}
		log.Printf("[eval] stage end workflow=%s stage=%s conversation=%s", wf.Id, models.AgentNova, novaConv.Id)
		result.Conversations = append(result.Conversations, novaConv)
		result.AgentTranscripts[models.AgentNova] = conversationTranscript(novaConv)
	}
	wf, err = collections.GetWorkflow(wf.Id)
	if err != nil {
		return result, err
	}
	log.Printf("[eval] workflow stage check workflow=%s current_stage=%s resolved=%t", wf.Id, wf.CurrentStage, wf.ResolvedAt != nil)
	if wf.CurrentStage == models.AgentDelta && wf.ResolvedAt == nil {
		deltaClient, err := clientForSimulation(models.AgentDelta, cfg.PromptOverrides)
		if err != nil {
			return result, err
		}
		log.Printf("[eval] stage begin workflow=%s stage=%s", wf.Id, models.AgentDelta)
		deltaConv, err := simulateDelta(ctx, persona, wf, deltaClient, personaType, seed, cfg.MaxTurnsPerAgent)
		if err != nil {
			return result, err
		}
		log.Printf("[eval] stage end workflow=%s stage=%s conversation=%s", wf.Id, models.AgentDelta, deltaConv.Id)
		result.Conversations = append(result.Conversations, deltaConv)
		result.AgentTranscripts[models.AgentDelta] = conversationTranscript(deltaConv)
	}
	result.Workflow = mustCurrentWorkflow(wf.Id, *wf)
	result.Transcript = fullTranscript(result.AgentTranscripts)
	return result, nil
}

func simulateAria(ctx context.Context, persona *personaSimulator, wf *models.BorrowerWorkflow, client *agents.Client, personaType models.Persona, seed string, maxTurns int) (models.AgentConversation, bool, error) {
	conv, err := createSimConversation(*wf, models.AgentAria, client.PromptVersion(), personaType, seed)
	if err != nil {
		return conv, false, err
	}
	log.Printf("[eval] aria conversation created workflow=%s conversation=%s prompt_version=%d", wf.Id, conv.Id, client.PromptVersion())
	messages := []models.AgentMessage{}
	nextBorrower, err := persona.Next(ctx, personaType, seed, models.AgentAria, "", personaOpeningInstruction(*wf, models.AgentAria), borrowerPersonaContext(*wf))
	if err != nil {
		return conv, false, err
	}
	log.Printf("[eval] aria opening persona=%s seed=%s text=%q", personaType, seed, previewText(nextBorrower, 120))
	for turn := 0; turn < maxTurns; turn++ {
		turnStart := time.Now()
		log.Printf("[eval] aria turn start workflow=%s conversation=%s turn=%d/%d borrower_chars=%d", wf.Id, conv.Id, turn+1, maxTurns, len(nextBorrower))
		borrowerMsg, err := insertMessage(conv, models.MessageRoleBorrower, nextBorrower, 0)
		if err != nil {
			return conv, false, err
		}
		messages = append(messages, borrowerMsg)
		handoff, err := collections.HandoffForStage(*wf)
		if err != nil {
			return conv, false, err
		}
		toolResults, resp, err := collections.ConverseForStage(client, *wf, models.AgentAria, handoff, messages)
		if err != nil {
			return conv, false, err
		}
		agentText := strings.TrimSpace(resp.AIResponse)
		if agentText == "" && toolResults.AriaHandoff != nil {
			agentText = "Thank you. Riverline will call at the scheduled time."
		}
		agentMsg, err := insertMessage(conv, models.MessageRoleAgent, agentText, resp.OutputTokens)
		if err != nil {
			return conv, false, err
		}
		messages = append(messages, agentMsg)
		_ = collections.LogCost("agent_response", &conv.AgentId, client.ModelUsed(), resp.InputTokens, resp.OutputTokens, &conv.Id, nil)
		log.Printf("[eval] aria turn agent workflow=%s conversation=%s turn=%d response_chars=%d tokens_in=%d tokens_out=%d handoff=%t duration=%s", wf.Id, conv.Id, turn+1, len(agentText), resp.InputTokens, resp.OutputTokens, toolResults.AriaHandoff != nil, time.Since(turnStart))
		if toolResults.AriaHandoff != nil {
			if err := collections.ApplyAriaHandoffForSimulation(wf, toolResults.AriaHandoff.Result); err != nil {
				return conv, false, err
			}
			if err := collections.CompleteARIA(wf.Id); err != nil {
				return conv, false, err
			}
			if err := finishConversation(conv.Id, countRole(messages, models.MessageRoleBorrower), totalTokens(messages)+toolResults.AriaHandoff.Tokens, nil); err != nil {
				return conv, false, err
			}
			log.Printf("[eval] aria handoff applied workflow=%s conversation=%s turns=%d handoff_tokens=%d", wf.Id, conv.Id, turn+1, toolResults.AriaHandoff.Tokens)
			conv = mustConversation(conv.Id, conv)
			return conv, true, nil
		}
		transcript := transcriptFromMessages(messages)
		nextBorrower, err = persona.Next(ctx, personaType, seed, models.AgentAria, transcript, personaReplyInstruction(*wf, models.AgentAria), borrowerPersonaContext(*wf))
		if err != nil {
			return conv, false, err
		}
		log.Printf("[eval] aria turn borrower next workflow=%s conversation=%s turn=%d next_chars=%d text=%q", wf.Id, conv.Id, turn+1, len(nextBorrower), previewText(nextBorrower, 120))
	}
	outcome := models.OutcomeNoResponse
	_ = finishConversation(conv.Id, countRole(messages, models.MessageRoleBorrower), totalTokens(messages), &outcome)
	log.Printf("[eval] aria max turns reached workflow=%s conversation=%s max_turns=%d", wf.Id, conv.Id, maxTurns)
	conv = mustConversation(conv.Id, conv)
	return conv, false, nil
}

func simulateNovaText(ctx context.Context, persona *personaSimulator, wf *models.BorrowerWorkflow, novaClient *agents.Client, deltaClient *agents.Client, personaType models.Persona, seed string, maxTurns int) (models.AgentConversation, error) {
	log.Printf("[eval] nova prepare start workflow=%s", wf.Id)
	offer, err := collections.PrepareNOVAWithClient(wf.Id, novaClient)
	if err != nil {
		return models.AgentConversation{}, err
	}
	_ = offer
	log.Printf("[eval] nova prepare done workflow=%s", wf.Id)
	wf, err = collections.GetWorkflow(wf.Id)
	if err != nil {
		return models.AgentConversation{}, err
	}
	conv, err := createSimConversation(*wf, models.AgentNova, novaClient.PromptVersion(), personaType, seed)
	if err != nil {
		return conv, err
	}
	log.Printf("[eval] nova conversation created workflow=%s conversation=%s prompt_version=%d context_chars=%d", wf.Id, conv.Id, novaClient.PromptVersion(), len(derefString(wf.ContextForNova)))
	handoff := novaSimulationHandoff(*wf)
	firstStart := time.Now()
	log.Printf("[eval] nova first turn start workflow=%s conversation=%s handoff_chars=%d", wf.Id, conv.Id, len(handoff))
	first, err := novaClient.GenerateTextWithContext(handoff, "The outbound call has connected. Produce NOVA's first borrower-facing spoken turn only.")
	if err != nil {
		return conv, err
	}
	log.Printf("[eval] nova first turn done workflow=%s conversation=%s response_chars=%d tokens_in=%d tokens_out=%d duration=%s", wf.Id, conv.Id, len(strings.TrimSpace(first.AIResponse)), first.InputTokens, first.OutputTokens, time.Since(firstStart))
	agentMsg, err := insertMessage(conv, models.MessageRoleAgent, strings.TrimSpace(first.AIResponse), first.OutputTokens)
	if err != nil {
		return conv, err
	}
	messages := []models.AgentMessage{agentMsg}
	_ = collections.LogCost("agent_response", &conv.AgentId, novaClient.ModelUsed(), first.InputTokens, first.OutputTokens, &conv.Id, nil)
	for turn := 0; turn < maxTurns; turn++ {
		turnStart := time.Now()
		log.Printf("[eval] nova turn start workflow=%s conversation=%s turn=%d/%d", wf.Id, conv.Id, turn+1, maxTurns)
		borrowerText, err := persona.Next(ctx, personaType, seed, models.AgentNova, transcriptFromMessages(messages), personaReplyInstruction(*wf, models.AgentNova), borrowerPersonaContext(*wf))
		if err != nil {
			return conv, err
		}
		borrowerMsg, err := insertMessage(conv, models.MessageRoleBorrower, borrowerText, 0)
		if err != nil {
			return conv, err
		}
		messages = append(messages, borrowerMsg)
		resp, err := novaClient.Converse(handoff, messages)
		if err != nil {
			return conv, err
		}
		agentMsg, err := insertMessage(conv, models.MessageRoleAgent, strings.TrimSpace(resp.AIResponse), resp.OutputTokens)
		if err != nil {
			return conv, err
		}
		messages = append(messages, agentMsg)
		_ = collections.LogCost("agent_response", &conv.AgentId, novaClient.ModelUsed(), resp.InputTokens, resp.OutputTokens, &conv.Id, nil)
		log.Printf("[eval] nova turn done workflow=%s conversation=%s turn=%d borrower_chars=%d response_chars=%d tokens_in=%d tokens_out=%d duration=%s", wf.Id, conv.Id, turn+1, len(borrowerText), len(strings.TrimSpace(resp.AIResponse)), resp.InputTokens, resp.OutputTokens, time.Since(turnStart))
	}
	transcript := transcriptFromMessages(messages)
	log.Printf("[eval] nova complete start workflow=%s conversation=%s transcript_chars=%d", wf.Id, conv.Id, len(transcript))
	if _, err := collections.CompleteNOVAWithClients(wf.Id, "simulated-"+seed, transcript, "", nil, nil, novaClient, deltaClient); err != nil {
		return conv, err
	}
	log.Printf("[eval] nova complete done workflow=%s conversation=%s", wf.Id, conv.Id)
	if err := finishConversation(conv.Id, countRole(messages, models.MessageRoleBorrower), totalTokens(messages), nil); err != nil {
		return conv, err
	}
	return mustConversation(conv.Id, conv), nil
}

func simulateDelta(ctx context.Context, persona *personaSimulator, wf *models.BorrowerWorkflow, client *agents.Client, personaType models.Persona, seed string, maxTurns int) (models.AgentConversation, error) {
	conv, err := createSimConversation(*wf, models.AgentDelta, client.PromptVersion(), personaType, seed)
	if err != nil {
		return conv, err
	}
	log.Printf("[eval] delta conversation created workflow=%s conversation=%s prompt_version=%d", wf.Id, conv.Id, client.PromptVersion())
	handoff, err := collections.HandoffForStage(*wf)
	if err != nil {
		return conv, err
	}
	firstStart := time.Now()
	log.Printf("[eval] delta first turn start workflow=%s conversation=%s handoff_chars=%d", wf.Id, conv.Id, len(handoff))
	first, err := client.GenerateTextWithContext(handoff, "Start the final notice chat now. Produce only the borrower-facing Riverline message.")
	if err != nil {
		return conv, err
	}
	log.Printf("[eval] delta first turn done workflow=%s conversation=%s response_chars=%d tokens_in=%d tokens_out=%d duration=%s", wf.Id, conv.Id, len(strings.TrimSpace(first.AIResponse)), first.InputTokens, first.OutputTokens, time.Since(firstStart))
	agentMsg, err := insertMessage(conv, models.MessageRoleAgent, strings.TrimSpace(first.AIResponse), first.OutputTokens)
	if err != nil {
		return conv, err
	}
	messages := []models.AgentMessage{agentMsg}
	_ = collections.LogCost("agent_response", &conv.AgentId, client.ModelUsed(), first.InputTokens, first.OutputTokens, &conv.Id, nil)
	for turn := 0; turn < maxTurns/2; turn++ {
		turnStart := time.Now()
		log.Printf("[eval] delta turn start workflow=%s conversation=%s turn=%d/%d", wf.Id, conv.Id, turn+1, maxTurns/2)
		borrowerText, err := persona.Next(ctx, personaType, seed, models.AgentDelta, transcriptFromMessages(messages), personaReplyInstruction(*wf, models.AgentDelta), borrowerPersonaContext(*wf))
		if err != nil {
			return conv, err
		}
		borrowerMsg, err := insertMessage(conv, models.MessageRoleBorrower, borrowerText, 0)
		if err != nil {
			return conv, err
		}
		messages = append(messages, borrowerMsg)
		resp, err := client.Converse(handoff, messages)
		if err != nil {
			return conv, err
		}
		agentMsg, err := insertMessage(conv, models.MessageRoleAgent, strings.TrimSpace(resp.AIResponse), resp.OutputTokens)
		if err != nil {
			return conv, err
		}
		messages = append(messages, agentMsg)
		_ = collections.LogCost("agent_response", &conv.AgentId, client.ModelUsed(), resp.InputTokens, resp.OutputTokens, &conv.Id, nil)
		log.Printf("[eval] delta turn done workflow=%s conversation=%s turn=%d borrower_chars=%d response_chars=%d tokens_in=%d tokens_out=%d duration=%s", wf.Id, conv.Id, turn+1, len(borrowerText), len(strings.TrimSpace(resp.AIResponse)), resp.InputTokens, resp.OutputTokens, time.Since(turnStart))
	}
	log.Printf("[eval] delta complete start workflow=%s conversation=%s", wf.Id, conv.Id)
	if _, err := collections.CompleteDeltaConversation(wf.Id, conv.Id, client); err != nil {
		return conv, err
	}
	log.Printf("[eval] delta complete done workflow=%s conversation=%s", wf.Id, conv.Id)
	return mustConversation(conv.Id, conv), nil
}

type personaSimulator struct {
	client *collections.LlmClient
}

func newPersonaSimulator(cfg constants.SelfLearningConfig) (*personaSimulator, error) {
	if strings.TrimSpace(cfg.PersonaLLMAPIKey) == "" {
		return nil, errors.New("PERSONA_LLM_API_KEY is required to run simulated conversations")
	}
	return &personaSimulator{client: collections.NewLLMClient(cfg.PersonaLLMBaseURL, cfg.PersonaLLMAPIKey, cfg.PersonaLLMModel)}, nil
}

func (p *personaSimulator) Next(ctx context.Context, persona models.Persona, seed string, stage models.AgentID, transcript string, instruction string, borrowerContext string) (string, error) {
	messages := []collections.LlmMessage{
		{Role: "system", Content: personaSystemPrompt(persona, seed, borrowerContext)},
		{Role: "user", Content: fmt.Sprintf("Stage: %s\nInstruction: %s\nTranscript so far:\n%s\n\nReturn only the next borrower message. No labels, JSON, tags, or commentary. Return a complete sentence; do not stop mid-number or mid-word.", stage, instruction, transcript)},
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		callStart := time.Now()
		log.Printf("[eval] persona call start persona=%s stage=%s seed=%s attempt=%d transcript_chars=%d context_chars=%d", persona, stage, seed, attempt+1, len(transcript), len(borrowerContext))
		callCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		resp, err := p.client.ChatWithTokenUsage(callCtx, messages, 0.35, 1000)
		cancel()
		if err != nil {
			lastErr = err
			log.Printf("[eval] persona call failed persona=%s stage=%s seed=%s attempt=%d duration=%s err=%v", persona, stage, seed, attempt+1, time.Since(callStart), err)
			continue
		}
		agentID := stage
		_ = collections.LogCost("simulation_persona", &agentID, "anthropic/"+resp.Model, resp.InputTokens, resp.OutputTokens, nil, nil)
		content := strings.TrimSpace(stripSpeakerLabel(resp.Content))
		if personaResponseComplete(content) && resp.StopReason != "max_tokens" {
			log.Printf("[eval] persona call done persona=%s stage=%s seed=%s attempt=%d model=%s stop_reason=%s tokens_in=%d tokens_out=%d chars=%d duration=%s text=%q", persona, stage, seed, attempt+1, resp.Model, resp.StopReason, resp.InputTokens, resp.OutputTokens, len(content), time.Since(callStart), previewText(content, 120))
			return content, nil
		}
		lastErr = fmt.Errorf("persona response incomplete: stop_reason=%s content=%q", resp.StopReason, content)
		log.Printf("[eval] persona call incomplete persona=%s stage=%s seed=%s attempt=%d model=%s stop_reason=%s tokens_in=%d tokens_out=%d chars=%d duration=%s text=%q", persona, stage, seed, attempt+1, resp.Model, resp.StopReason, resp.InputTokens, resp.OutputTokens, len(content), time.Since(callStart), previewText(content, 120))
	}
	return "", lastErr
}

func evaluateWithJudge(evaluator models.EvaluatorVersion, transcript string, judge constants.EvaluatorJudgeConfig) (MetricScores, int, string, error) {
	modelCfg := ai.ModelConfig{BaseModel: ai.BaseModel(judge.Model), Provider: ai.Provider(judge.Provider)}
	client := ai.NewKarmaAI(
		ai.BaseModel(judge.Model),
		ai.Provider(judge.Provider),
		ai.WithMaxTokens(1000),
		ai.WithTemperature(judge.Temperature),
	)
	var metrics MetricScores
	p := parser.NewParser(parser.WithAIClient(client), parser.WithMaxRetries(6))
	tokens, err := parseWithParser(p, buildEvaluationPrompt(evaluator, transcript), &metrics)
	if err != nil {
		return MetricScores{}, 0, "", fmt.Errorf("judge %s evaluate %s: %w", judge.Name, evaluator.AgentId, err)
	}
	normalizeMetrics(&metrics)
	return metrics, tokens, string(judge.Provider) + "/" + modelCfg.GetModelString(), nil
}

func parseWithParser(p *parser.Parser, prompt string, output any) (int, error) {
	_, tokens, err := p.Parse(prompt, "", output)
	return tokens, err
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
	raw := (scores.IdentityVerified + scores.InfoCompleteness + scores.NoRedundancy + scores.ToneAppropriateness + scores.OfferClarity + scores.ObjectionHandling + scores.CommitmentAttempt + scores.ContextContinuity + scores.ConsequenceAccuracy + scores.DeadlineSpecificity + scores.NoNegotiationDrift + 2*scores.CompliancePass) / 14 * 10
	if scores.CompliancePass == 0 {
		return math.Min(30, raw)
	}
	return raw
}

func aggregateJudgeResults(results []JudgeResult) MetricScores {
	var totalWeight float64
	out := MetricScores{ComplianceBreakdown: map[string]any{}}
	minComposite := math.MaxFloat64
	maxComposite := 0.0
	allCompliance := true
	for _, result := range results {
		w := result.Weight
		if w <= 0 {
			w = 1
		}
		totalWeight += w
		m := result.Metrics
		out.CompositeScore += m.CompositeScore * w
		out.IdentityVerified += m.IdentityVerified * w
		out.InfoCompleteness += m.InfoCompleteness * w
		out.NoRedundancy += m.NoRedundancy * w
		out.ToneAppropriateness += m.ToneAppropriateness * w
		out.OfferClarity += m.OfferClarity * w
		out.ObjectionHandling += m.ObjectionHandling * w
		out.CommitmentAttempt += m.CommitmentAttempt * w
		out.ContextContinuity += m.ContextContinuity * w
		out.ConsequenceAccuracy += m.ConsequenceAccuracy * w
		out.DeadlineSpecificity += m.DeadlineSpecificity * w
		out.NoNegotiationDrift += m.NoNegotiationDrift * w
		if m.CompliancePass <= 0 {
			allCompliance = false
		}
		minComposite = math.Min(minComposite, m.CompositeScore)
		maxComposite = math.Max(maxComposite, m.CompositeScore)
		if out.Reasoning != "" {
			out.Reasoning += " | "
		}
		out.Reasoning += result.Name + ": " + m.Reasoning
	}
	if totalWeight == 0 {
		totalWeight = 1
	}
	out.CompositeScore /= totalWeight
	out.IdentityVerified /= totalWeight
	out.InfoCompleteness /= totalWeight
	out.NoRedundancy /= totalWeight
	out.ToneAppropriateness /= totalWeight
	out.OfferClarity /= totalWeight
	out.ObjectionHandling /= totalWeight
	out.CommitmentAttempt /= totalWeight
	out.ContextContinuity /= totalWeight
	out.ConsequenceAccuracy /= totalWeight
	out.DeadlineSpecificity /= totalWeight
	out.NoNegotiationDrift /= totalWeight
	if allCompliance {
		out.CompliancePass = 10
	} else {
		out.CompliancePass = 0
		out.CompositeScore = math.Min(out.CompositeScore, 30)
	}
	if len(results) > 1 {
		out.JudgeBComposite = results[1].Metrics.CompositeScore
	} else {
		out.JudgeBComposite = out.CompositeScore
	}
	if minComposite == math.MaxFloat64 {
		minComposite = out.CompositeScore
	}
	out.JudgeDisagreement = maxComposite - minComposite
	out.ComplianceBreakdown["all_judges_compliance_passed"] = allCompliance
	normalizeMetrics(&out)
	return out
}

func clientForSimulation(agentID models.AgentID, overrides map[models.AgentID]PromptOverride) (*agents.Client, error) {
	if override, ok := overrides[agentID]; ok && strings.TrimSpace(override.PromptText) != "" {
		return agents.NewWithPrompt(agentID, override.VersionNumber, override.PromptText, agents.DefaultConfig(agentID))
	}
	switch agentID {
	case models.AgentNova:
		return agents.NewNova()
	case models.AgentDelta:
		return agents.NewDelta()
	default:
		return agents.NewAria()
	}
}

func createSimulatedWorkflow(persona models.Persona, seed string) (*models.BorrowerWorkflow, error) {
	now := time.Now().UTC()
	userID := "sim-user-" + seed
	loanID := "sim-loan-" + seed
	phone := "+15555559999"
	user := models.User{
		Id:        userID,
		FirstName: "Kartik",
		LastName:  "User",
		Email:     userID + "@simulation.local",
		Phone:     &phone,
		Dob:       time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC),
		Gender:    "unspecified",
		Extra:     map[string]any{"simulated": true, "persona": persona, "seed": seed},
		CreatedAt: now,
		UpdatedAt: now,
	}
	userOrm := orm.Load(&models.User{})
	defer userOrm.Close()
	if err := userOrm.Insert(&user); err != nil {
		return nil, err
	}
	lastPayment := now.AddDate(0, -3, 0)
	lastAmount := 300.0
	interest := 14.25
	loan := models.Loan{
		Id:                   loanID,
		UserId:               userID,
		AccountNumberPartial: "6789",
		LoanType:             "personal",
		PrincipalAmount:      15000,
		OutstandingAmount:    9825,
		DaysOverdue:          74,
		LastPaymentDate:      &lastPayment,
		LastPaymentAmount:    &lastAmount,
		InterestRate:         &interest,
		PolicyMaxDiscountPct: 22,
		Status:               models.BorrowerStatusPending,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	loanOrm := orm.Load(&models.Loan{})
	defer loanOrm.Close()
	if err := loanOrm.Insert(&loan); err != nil {
		return nil, err
	}
	summary := fmt.Sprintf("Borrower %s %s has a %s loan ending %s. Outstanding amount is %.2f. Principal amount is %.2f. The loan is %d days overdue. Policy max discount is %.2f%%. Account status is %s.", user.FirstName, user.LastName, loan.LoanType, loan.AccountNumberPartial, loan.OutstandingAmount, loan.PrincipalAmount, loan.DaysOverdue, loan.PolicyMaxDiscountPct, loan.Status)
	wf := &models.BorrowerWorkflow{
		Id:                 "sim-wf-" + seed,
		UserId:             userID,
		LoanId:             loanID,
		CurrentStage:       models.AgentAria,
		AriaAttempts:       0,
		IdentityVerified:   boolPtr(false),
		HardshipMentioned:  boolPtr(false),
		StopContactFlagged: boolPtr(false),
		HardshipFlagged:    boolPtr(false),
		AriaSummary:        stringPtr(summary),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	wfOrm := orm.Load(&models.BorrowerWorkflow{})
	defer wfOrm.Close()
	if err := wfOrm.Insert(wf); err != nil {
		return nil, err
	}
	return wf, nil
}

func createSimConversation(wf models.BorrowerWorkflow, agentID models.AgentID, promptVersion int, persona models.Persona, seed string) (models.AgentConversation, error) {
	conv := models.AgentConversation{
		Id:              utils.GenerateID(),
		WorkflowId:      wf.Id,
		UserId:          wf.UserId,
		AgentId:         agentID,
		IsSimulated:     boolPtr(true),
		PersonaType:     &persona,
		Seed:            &seed,
		PromptVersion:   promptVersion,
		TotalTurns:      intPtr(0),
		TotalTokensUsed: intPtr(0),
		StartedAt:       time.Now().UTC(),
	}
	o := orm.Load(&models.AgentConversation{})
	defer o.Close()
	return conv, o.Insert(&conv)
}

func insertMessage(conv models.AgentConversation, role models.MessageRole, content string, tokens int) (models.AgentMessage, error) {
	tokenPtr := (*int)(nil)
	if tokens > 0 {
		tokenPtr = &tokens
	}
	msg := models.AgentMessage{
		Id:             utils.GenerateID(),
		ConversationId: conv.Id,
		WorkflowId:     conv.WorkflowId,
		AgentId:        conv.AgentId,
		Role:           role,
		Content:        strings.TrimSpace(content),
		TokenCount:     tokenPtr,
		CreatedAt:      time.Now().UTC(),
	}
	if msg.Content == "" {
		msg.Content = "No substantive response recorded."
	}
	o := orm.Load(&models.AgentMessage{})
	defer o.Close()
	return msg, o.Insert(&msg)
}

func finishConversation(conversationID string, turns int, tokens int, outcome *models.Outcome) error {
	conv, err := collections.ConversationByIDOrWorkflow(conversationID)
	if err != nil {
		return err
	}
	ended := time.Now().UTC()
	conv.Conversation.TotalTurns = &turns
	conv.Conversation.TotalTokensUsed = &tokens
	conv.Conversation.EndedAt = &ended
	if outcome != nil {
		conv.Conversation.Outcome = outcome
	}
	o := orm.Load(&models.AgentConversation{})
	defer o.Close()
	return o.Update(&conv.Conversation, conversationID)
}

func conversationTranscript(conv models.AgentConversation) string {
	messages, err := collections.ListMessages(conv.Id, conv.WorkflowId)
	if err != nil {
		return ""
	}
	return transcriptFromMessages(messages)
}

func transcriptFromMessages(messages []models.AgentMessage) string {
	var b strings.Builder
	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if strings.HasPrefix(content, "NOVA outbound call started with handoff:") {
			continue
		}
		if strings.HasPrefix(content, "NOVA call completed. Transcript:") {
			b.WriteString(strings.TrimSpace(strings.TrimPrefix(content, "NOVA call completed. Transcript:")))
			b.WriteByte('\n')
			continue
		}
		label := "Agent"
		if msg.Role == models.MessageRoleBorrower {
			label = "Borrower"
		}
		b.WriteString(label)
		b.WriteString(": ")
		b.WriteString(content)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

func fullTranscript(byAgent map[models.AgentID]string) string {
	order := []models.AgentID{models.AgentAria, models.AgentNova, models.AgentDelta}
	parts := make([]string, 0, len(order))
	for _, agentID := range order {
		if transcript := strings.TrimSpace(byAgent[agentID]); transcript != "" {
			parts = append(parts, strings.ToUpper(string(agentID))+" TRANSCRIPT\n"+transcript)
		}
	}
	return strings.Join(parts, "\n\n")
}

func personaSystemPrompt(persona models.Persona, seed string, borrowerContext string) string {
	return fmt.Sprintf(`You are simulating a borrower in Riverline's debt collection assessment. You are not the Riverline agent.

Authoritative borrower and loan row context:
%s

Persona: %s.
Seed: %s.

Behavior rules:
- Stay in character as the borrower.
- Never write the agent's lines.
- Do not output labels like "Borrower:".
- Keep replies short and realistic.
- For cooperative, answer directly and accept a reasonable plan if clearly presented.
- For combative, resist and challenge but do not invent legal facts.
- For evasive, avoid exact details until pressed, then provide partial answers.
- For confused, ask clarifying questions and misunderstand one important point.
- For distressed, mention hardship or crisis pressure and avoid overcommitting.
- If the agent asks for stop-contact handling, only request stop contact if it fits the persona trajectory.`, borrowerContext, persona, seed)
}

func borrowerPersonaContext(wf models.BorrowerWorkflow) string {
	user, _ := collections.GetUser(wf.UserId)
	loan, _ := collections.GetLoan(wf.LoanId)
	if user == nil || loan == nil {
		return "Borrower row context unavailable. Use the visible transcript only."
	}
	return fmt.Sprintf(`users row:
- id: %s
- name: %s %s
- email: %s
- phone: %s
- extra: %s

loans row:
- id: %s
- user_id: %s
- account_number_partial: %s
- loan_type: %s
- outstanding_amount: %.2f
- principal_amount: %.2f
- days_overdue: %d
- policy_max_discount_pct: %.2f
- status: %s`,
		user.Id,
		user.FirstName,
		user.LastName,
		user.Email,
		derefString(user.Phone),
		MarshalJSON(user.Extra),
		loan.Id,
		loan.UserId,
		loan.AccountNumberPartial,
		loan.LoanType,
		loan.OutstandingAmount,
		loan.PrincipalAmount,
		loan.DaysOverdue,
		loan.PolicyMaxDiscountPct,
		loan.Status,
	)
}

func personaResponseComplete(content string) bool {
	content = strings.TrimSpace(content)
	if len(content) < 8 {
		return false
	}
	last := content[len(content)-1]
	return strings.ContainsAny(string(last), ".?!")
}

func personaOpeningInstruction(wf models.BorrowerWorkflow, stage models.AgentID) string {
	return fmt.Sprintf("Start the %s interaction. The borrower is entering the Riverline workflow for loan %s.", stage, wf.LoanId)
}

func personaReplyInstruction(wf models.BorrowerWorkflow, stage models.AgentID) string {
	return fmt.Sprintf("Reply to the latest Riverline message in the %s stage. Keep continuity with workflow %s.", stage, wf.Id)
}

func novaSimulationHandoff(wf models.BorrowerWorkflow) string {
	return fmt.Sprintf("Simulated NOVA text-call runtime. Current IST time: %s. Workflow ID: %s. Use only this NOVA runtime context: %s", time.Now().In(istLocation()).Format(time.RFC3339), wf.Id, derefString(wf.ContextForNova))
}

func stripSpeakerLabel(value string) string {
	value = strings.TrimSpace(value)
	for _, prefix := range []string{"Borrower:", "User:", "Customer:"} {
		if strings.HasPrefix(value, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(value, prefix))
		}
	}
	return value
}

func previewText(value string, maxLen int) string {
	value = strings.Join(strings.Fields(value), " ")
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	return value[:maxLen] + "..."
}

func generateCandidatePrompt(agentID models.AgentID, currentPrompt string) (string, int, string, error) {
	client, err := clientForSimulation(agentID, nil)
	if err != nil {
		return "", 0, "", err
	}
	prompt := fmt.Sprintf(`Generate an improved production system prompt for the %s collections agent.

Current prompt:
%s

Keep the same agent role, tools, compliance boundaries, context budgets, borrower-facing single Riverline identity, and handoff responsibilities. Improve measurable performance only where the current prompt is likely to cause missing disclosures, weak continuity, vague offers, skipped callback scheduling, weak objection handling, or compliance risk.

Return only the complete replacement system prompt.`, agentID, currentPrompt)
	resp, err := client.GenerateText(prompt)
	if err != nil {
		return "", 0, "", fmt.Errorf("generate candidate prompt for %s: %w", agentID, err)
	}
	return strings.TrimSpace(resp.AIResponse), resp.Tokens, client.ModelUsed(), nil
}

func saveCandidatePrompt(agentID models.AgentID, version int, candidatePrompt string, adopted bool, exp *models.PromptExperiment) error {
	current, err := collections.ActivePromptVersion(agentID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	var adoptionReason *string
	var rejectionReason *string
	if adopted {
		reason := fmt.Sprintf("adopted by prompt experiment %s: delta=%.2f p=%.4f d=%.2f compliance %.2f->%.2f", exp.Id, exp.MeanDelta, exp.PValue, derefFloat(exp.CohensD), exp.ControlComplianceRate, exp.TreatmentComplianceRate)
		adoptionReason = &reason
	} else {
		rejectionReason = exp.RejectionReason
	}
	candidate := models.PromptVersion{
		Id:              utils.GenerateID(),
		AgentId:         agentID,
		VersionNumber:   version,
		PromptText:      candidatePrompt,
		IsActive:        adopted,
		AdoptionReason:  adoptionReason,
		RejectionReason: rejectionReason,
		CreatedAt:       now,
	}
	if adopted {
		candidate.AdoptedAt = &now
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
	if adopted && agentID == models.AgentNova {
		if err := collections.SyncNovaVapiAssistant(context.Background()); err != nil {
			return err
		}
	}
	return nil
}

func resolveMetaFlag(flag *models.MetaFlag) error {
	agentID := derefAgent(flag.AgentId)
	before, err := activeEvaluatorVersion(agentID)
	if err != nil {
		return err
	}
	judgePrompt, tokens, modelUsed, err := generateEvaluatorRevision(agentID, *before, *flag)
	if err != nil {
		return err
	}
	nextVersion, err := nextEvaluatorVersion(agentID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	active := true
	changeReason := "Generated from meta-evaluation flag " + flag.Id
	evaluator := models.EvaluatorVersion{
		Id:                utils.GenerateID(),
		VersionNumber:     nextVersion,
		AgentId:           agentID,
		JudgePrompt:       judgePrompt,
		IsActive:          &active,
		ChangeReason:      &changeReason,
		TriggeredByFlagId: &flag.Id,
		CreatedAt:         now,
	}
	evaluatorOrm := orm.Load(&models.EvaluatorVersion{})
	defer evaluatorOrm.Close()
	inactive := false
	var existing []models.EvaluatorVersion
	if err := evaluatorOrm.GetByFieldEquals("AgentId", agentID).Scan(&existing); err != nil {
		return err
	}
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
	resolution := fmt.Sprintf("Created and activated evaluator version %d from meta-evaluation evidence.", nextVersion)
	flag.Resolved = &resolved
	flag.Resolution = &resolution
	flag.EvaluatorVersionBefore = &before.VersionNumber
	flag.EvaluatorVersionAfter = &nextVersion
	flag.ResolvedAt = &now
	flagOrm := orm.Load(&models.MetaFlag{})
	defer flagOrm.Close()
	if err := flagOrm.Update(flag, flag.Id); err != nil {
		return err
	}
	return collections.LogCost("evaluator_prompt_generation", flag.AgentId, modelUsed, tokens, 0, nil, nil)
}

func generateEvaluatorRevision(agentID models.AgentID, current models.EvaluatorVersion, flag models.MetaFlag) (string, int, string, error) {
	client, err := clientForSimulation(agentID, nil)
	if err != nil {
		return "", 0, "", err
	}
	evidence, _ := json.Marshal(flag.Evidence)
	prompt := fmt.Sprintf(`Generate a revised evaluator judge prompt for the %s collections agent.

Current evaluator prompt:
%s

Meta-evaluation flag: %s
Evidence: %s
Proposed action: %s

The revised prompt must:
- Return only JSON score outputs when used by an evaluator.
- Preserve the existing schema exactly.
- Add concrete low, medium, and high scoring anchors.
- Penalize vague compliance, repeated questions, missing disclosures, false threats, privacy leaks, harassment, ignored hardship, negotiation drift, and poor handoff continuity.
- Improve stable rerun behavior for identical transcripts.

Return only the revised judge prompt text.`, agentID, current.JudgePrompt, flag.FlagType, string(evidence), derefString(flag.ProposedAction))
	resp, err := client.GenerateText(prompt)
	if err != nil {
		return "", 0, "", fmt.Errorf("generate evaluator revision for %s: %w", agentID, err)
	}
	return strings.TrimSpace(resp.AIResponse), resp.Tokens, client.ModelUsed(), nil
}

func conversationsForRerun(req RerunRequest) ([]models.AgentConversation, error) {
	o := orm.Load(&models.AgentConversation{})
	defer o.Close()
	seen := map[string]bool{}
	out := []models.AgentConversation{}
	for _, id := range req.ConversationIDs {
		var rows []models.AgentConversation
		if err := o.GetByFieldEquals("Id", id).Scan(&rows); err != nil {
			return nil, err
		}
		for _, row := range rows {
			if !seen[row.Id] {
				out = append(out, row)
				seen[row.Id] = true
			}
		}
	}
	for _, workflowID := range req.WorkflowIDs {
		var rows []models.AgentConversation
		if err := o.GetByFieldEquals("WorkflowId", workflowID).Scan(&rows); err != nil {
			return nil, err
		}
		for _, row := range rows {
			if !seen[row.Id] {
				out = append(out, row)
				seen[row.Id] = true
			}
		}
	}
	if len(req.ConversationIDs) == 0 && len(req.WorkflowIDs) == 0 {
		var rows []models.AgentConversation
		if err := o.GetAll().Scan(&rows); err != nil {
			return nil, err
		}
		out = rows
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out, nil
}

func scoresForAgent(agentID models.AgentID) ([]models.ConversationScore, error) {
	o := orm.Load(&models.ConversationScore{})
	defer o.Close()
	var scores []models.ConversationScore
	if err := o.GetByFieldEquals("AgentId", agentID).Scan(&scores); err != nil {
		return nil, err
	}
	return scores, nil
}

func aggregateScoreRows(rows []models.ConversationScore) MetricAggregate {
	values := make([]float64, 0, len(rows))
	disagreements := make([]float64, 0, len(rows))
	compliance := 0
	simulated := 0
	for _, row := range rows {
		values = append(values, row.CompositeScore)
		if row.JudgeDisagreementDelta != nil {
			disagreements = append(disagreements, *row.JudgeDisagreementDelta)
		}
		if row.CompliancePassed != nil && *row.CompliancePassed {
			compliance++
		}
		if row.IsSimulated != nil && *row.IsSimulated {
			simulated++
		}
	}
	n := len(rows)
	if n == 0 {
		return MetricAggregate{}
	}
	return MetricAggregate{
		N:                 n,
		Mean:              Mean(values),
		Stddev:            Stddev(values),
		Median:            ComputePercentile(values, 50),
		ComplianceRate:    float64(compliance) / float64(n),
		MeanDisagreement:  Mean(disagreements),
		SimulatedFraction: float64(simulated) / float64(n),
	}
}

func newMetaFlag(flagType models.FlagType, agentID models.AgentID, evidence map[string]any, action string) models.MetaFlag {
	resolved := false
	return models.MetaFlag{Id: utils.GenerateID(), FlagType: flagType, AgentId: &agentID, Evidence: evidence, ProposedAction: &action, Resolved: &resolved, CreatedAt: time.Now().UTC()}
}

func scoreValues(scores []models.ConversationScore) []float64 {
	values := make([]float64, 0, len(scores))
	for _, score := range scores {
		values = append(values, score.CompositeScore)
	}
	return values
}

func lowestMetricStddev(scores []models.ConversationScore) (string, float64) {
	metrics := map[string][]float64{}
	for _, score := range scores {
		appendPtrMetric(metrics, "identity_verified", score.ScoreIdentityVerified)
		appendPtrMetric(metrics, "info_completeness", score.ScoreInfoCompleteness)
		appendPtrMetric(metrics, "no_redundancy", score.ScoreNoRedundancy)
		appendPtrMetric(metrics, "tone_appropriateness", score.ScoreToneAppropriateness)
		appendPtrMetric(metrics, "offer_clarity", score.ScoreOfferClarity)
		appendPtrMetric(metrics, "objection_handling", score.ScoreObjectionHandling)
		appendPtrMetric(metrics, "commitment_attempt", score.ScoreCommitmentAttempt)
		appendPtrMetric(metrics, "context_continuity", score.ScoreContextContinuity)
		appendPtrMetric(metrics, "consequence_accuracy", score.ScoreConsequenceAccuracy)
		appendPtrMetric(metrics, "deadline_specificity", score.ScoreDeadlineSpecificity)
		appendPtrMetric(metrics, "no_negotiation_drift", score.ScoreNoNegotiationDrift)
	}
	bestMetric := ""
	bestStddev := math.MaxFloat64
	for metric, values := range metrics {
		if len(values) < 3 {
			continue
		}
		stddev := Stddev(values)
		if stddev < bestStddev {
			bestMetric = metric
			bestStddev = stddev
		}
	}
	if bestStddev == math.MaxFloat64 {
		return "", 0
	}
	return bestMetric, bestStddev
}

func appendPtrMetric(metrics map[string][]float64, key string, value *float64) {
	if value != nil {
		metrics[key] = append(metrics[key], *value)
	}
}

func judgeDisagreementStats(scores []models.ConversationScore, threshold float64) (float64, float64) {
	if len(scores) == 0 {
		return 0, 0
	}
	diverged := 0
	values := make([]float64, 0, len(scores))
	for _, score := range scores {
		if score.JudgeDisagreementDelta == nil {
			continue
		}
		values = append(values, *score.JudgeDisagreementDelta)
		if *score.JudgeDisagreementDelta > threshold {
			diverged++
		}
	}
	if len(values) == 0 {
		return 0, 0
	}
	return float64(diverged) / float64(len(values)), Mean(values)
}

func postAdoptionRegression(scores []models.ConversationScore) map[string]any {
	byVersion := map[int][]float64{}
	for _, score := range scores {
		byVersion[score.PromptVersion] = append(byVersion[score.PromptVersion], score.CompositeScore)
	}
	if len(byVersion) < 2 {
		return nil
	}
	versions := make([]int, 0, len(byVersion))
	for version := range byVersion {
		versions = append(versions, version)
	}
	sort.Ints(versions)
	latest := versions[len(versions)-1]
	previous := versions[len(versions)-2]
	if len(byVersion[latest]) < 3 || len(byVersion[previous]) < 3 {
		return nil
	}
	oldMean := Mean(byVersion[previous])
	newMean := Mean(byVersion[latest])
	if newMean <= oldMean-5 {
		return map[string]any{"previous_version": previous, "latest_version": latest, "old_mean": oldMean, "new_mean": newMean, "delta": newMean - oldMean}
	}
	return nil
}

func aggregateSimulationMeans(stats []SimulationScore) []float64 {
	out := make([]float64, 0, len(stats))
	for _, row := range stats {
		out = append(out, row.Mean)
	}
	return out
}

func aggregateComplianceRate(stats []SimulationScore) float64 {
	if len(stats) == 0 {
		return 0
	}
	total := 0.0
	for _, row := range stats {
		total += row.ComplianceRate
	}
	return total / float64(len(stats))
}

func rejectionReason(adopt bool, pValue, delta, effectSize, controlCompliance, treatmentCompliance float64) *string {
	if adopt {
		return nil
	}
	reason := fmt.Sprintf("candidate rejected: delta=%.2f p=%.4f d=%.2f compliance %.2f->%.2f did not clear adoption gates", delta, pValue, effectSize, controlCompliance, treatmentCompliance)
	return &reason
}

func nextPromptVersion(agentID models.AgentID) (int, error) {
	o := orm.Load(&models.PromptVersion{})
	defer o.Close()
	var rows []models.PromptVersion
	if err := o.GetByFieldEquals("AgentId", agentID).Scan(&rows); err != nil {
		return 0, err
	}
	maxVersion := 0
	for _, row := range rows {
		if row.VersionNumber > maxVersion {
			maxVersion = row.VersionNumber
		}
	}
	return maxVersion + 1, nil
}

func nextEvaluatorVersion(agentID models.AgentID) (int, error) {
	o := orm.Load(&models.EvaluatorVersion{})
	defer o.Close()
	var rows []models.EvaluatorVersion
	if err := o.GetByFieldEquals("AgentId", agentID).Scan(&rows); err != nil {
		return 0, err
	}
	maxVersion := 0
	for _, row := range rows {
		if row.VersionNumber > maxVersion {
			maxVersion = row.VersionNumber
		}
	}
	return maxVersion + 1, nil
}

func normalizeJudges(judges []constants.EvaluatorJudgeConfig) []constants.EvaluatorJudgeConfig {
	out := append([]constants.EvaluatorJudgeConfig(nil), judges...)
	if len(out) == 0 {
		return constants.DefaultSelfLearningConfig().Judges
	}
	for i := range out {
		if out[i].Name == "" {
			out[i].Name = fmt.Sprintf("judge_%d", i+1)
		}
		if out[i].Provider == "" {
			out[i].Provider = "groq"
		}
		if out[i].Model == "" {
			out[i].Model = "llama-3.3-70b"
		}
		if out[i].Weight <= 0 {
			out[i].Weight = 1
		}
	}
	return out
}

func defaultPersonas() []models.Persona {
	return []models.Persona{models.PersonaCooperative, models.PersonaCombative, models.PersonaEvasive, models.PersonaDistressed, models.PersonaConfused}
}

func simulationSeed(seed int64, persona models.Persona, index int) string {
	if seed == 0 {
		seed = time.Now().UTC().Unix()
	}
	return fmt.Sprintf("%d-%s-%d-%s", seed, persona, index, utils.GenerateID())
}

func countRole(messages []models.AgentMessage, role models.MessageRole) int {
	count := 0
	for _, msg := range messages {
		if msg.Role == role {
			count++
		}
	}
	return count
}

func totalTokens(messages []models.AgentMessage) int {
	total := 0
	for _, msg := range messages {
		if msg.TokenCount != nil {
			total += *msg.TokenCount
		}
	}
	return total
}

func mustCurrentWorkflow(id string, fallback models.BorrowerWorkflow) models.BorrowerWorkflow {
	wf, err := collections.GetWorkflow(id)
	if err != nil {
		return fallback
	}
	return *wf
}

func mustConversation(id string, fallback models.AgentConversation) models.AgentConversation {
	view, err := collections.ConversationByIDOrWorkflow(id)
	if err != nil {
		return fallback
	}
	return view.Conversation
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
func derefBool(v *bool) bool      { return v != nil && *v }
func derefFloat(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}
func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func derefAgent(v *models.AgentID) models.AgentID {
	if v == nil {
		return models.AgentAria
	}
	return *v
}

func istLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err == nil {
		return loc
	}
	return time.FixedZone("IST", 5*60*60+30*60)
}
