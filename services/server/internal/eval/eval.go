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
	"sync"
	"time"

	"riverline_server/constants"
	"riverline_server/internal/agents"
	"riverline_server/internal/collections"
	"riverline_server/internal/models"

	"github.com/MelloB1989/karma/ai"
	karmaModels "github.com/MelloB1989/karma/models"
	"github.com/MelloB1989/karma/utils"
	"github.com/MelloB1989/karma/v2/orm"
	"github.com/openai/openai-go/v3/shared"
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

const (
	judgeCallTimeout          = 3600 * time.Second
	judgeCallTimeoutSlow      = 6600 * time.Second
	internalGenerationTimeout = 90 * time.Second
	aiCallMaxAttempts         = 3
	judgeJSONParseMaxAttempts = 4
)

var (
	nvidiaNIMRequestsPerMinute = constants.AppCfg.Get().NvidiaNIMRPM
	unavailableJudgeModels     sync.Map
	providerRateLimitUntil     sync.Map
)

func init() {
	if nvidiaNIMRequestsPerMinute > 0 {
		ai.SetGlobalRateLimit(ai.NvidiaNIM, nvidiaNIMRequestsPerMinute, ai.RateLimitBehaviorWait)
	}
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
		if !simulationReadyForSystemScoring(sim) {
			log.Printf("[eval] immediate scoring skipped workflow=%s seed=%s reason=incomplete_system_flow sections=%v error=%v", sim.Workflow.Id, sim.Seed, transcriptSectionsPresent(sim.AgentTranscripts), sim.Metadata["simulation_error"])
			return nil
		}
		simScores, err := ScoreSimulationsForAgent([]SimulatedConversation{sim}, cfg.AgentID, judges)
		if err != nil {
			return err
		}
		scores = append(scores, simScores...)
		return nil
	})
	if err != nil {
		return conversations, scores, err
	}
	if len(conversations) > 0 && len(scores) == 0 {
		return conversations, scores, errors.New("no complete simulations reached ARIA, NOVA, and DELTA handoff; judges were not run on partial transcripts")
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
			if err := enforceRunBudget(cfg); err != nil {
				return out, err
			}
			seed := simulationSeed(cfg.Seed, personaType, i)
			itemStart := time.Now()
			log.Printf("[eval] simulation start persona=%s index=%d/%d seed=%s", personaType, i+1, cfg.BatchSize, seed)
			sim, err := runOneSimulation(context.Background(), persona, cfg, personaType, seed)
			if err != nil {
				log.Printf("[eval] simulation failed persona=%s index=%d seed=%s duration=%s err=%v", personaType, i+1, seed, time.Since(itemStart), err)
				if sim.Workflow.Id == "" && len(sim.Conversations) == 0 {
					return out, err
				}
				if sim.Metadata == nil {
					sim.Metadata = map[string]any{}
				}
				sim.Metadata["simulation_error"] = err.Error()
				if strings.TrimSpace(sim.Transcript) == "" && len(sim.AgentTranscripts) > 0 {
					sim.Transcript = fullTranscript(sim.AgentTranscripts)
				}
				log.Printf("[eval] simulation partial preserved persona=%s index=%d seed=%s workflow=%s convs=%d", personaType, i+1, seed, sim.Workflow.Id, len(sim.Conversations))
			} else {
				log.Printf("[eval] simulation done persona=%s index=%d seed=%s workflow=%s convs=%d duration=%s", personaType, i+1, seed, sim.Workflow.Id, len(sim.Conversations), time.Since(itemStart))
			}
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

func enforceRunBudget(cfg SimConfig) error {
	if cfg.MaxRunCostUSD <= 0 {
		return nil
	}
	cost, err := currentTotalCostUSD()
	if err != nil {
		return err
	}
	spent := cost - cfg.BaseRunCostUSD
	if spent >= cfg.MaxRunCostUSD {
		return fmt.Errorf("eval run cost budget exceeded: spent=$%.4f budget=$%.4f", spent, cfg.MaxRunCostUSD)
	}
	return nil
}

func ScoreSimulationsForAgent(conversations []SimulatedConversation, agentID models.AgentID, judges []constants.EvaluatorJudgeConfig) ([]SimulationScore, error) {
	start := time.Now()
	log.Printf("[eval] scoring simulations start agent=%s count=%d judges=%d", agentID, len(conversations), len(judges))
	out := make([]SimulationScore, 0, len(conversations))
	for simIndex, sim := range conversations {
		simStart := time.Now()
		log.Printf("[eval] scoring simulation start agent=%s index=%d/%d workflow=%s seed=%s persona=%s convs=%d", agentID, simIndex+1, len(conversations), sim.Workflow.Id, sim.Seed, sim.Persona, len(sim.Conversations))
		if !simulationReadyForSystemScoring(sim) {
			log.Printf("[eval] scoring simulation skipped agent=%s workflow=%s reason=incomplete_system_flow sections=%v error=%v", agentID, sim.Workflow.Id, transcriptSectionsPresent(sim.AgentTranscripts), sim.Metadata["simulation_error"])
			continue
		}
		conv, ok := conversationForAgent(sim, agentID)
		if !ok {
			var err error
			conv, err = createEvaluationAnchorConversation(sim, agentID)
			if err != nil {
				return nil, err
			}
			log.Printf("[eval] scoring simulation anchored agent=%s workflow=%s conversation=%s reason=target_stage_not_reached", agentID, sim.Workflow.Id, conv.Id)
		}
		transcript := strings.TrimSpace(sim.Transcript)
		if transcript == "" {
			transcript = fullTranscript(sim.AgentTranscripts)
		}
		if transcript == "" {
			log.Printf("[eval] scoring simulation skipped agent=%s workflow=%s reason=empty_system_transcript", agentID, sim.Workflow.Id)
			continue
		}
		log.Printf("[eval] scoring workflow start agent=%s workflow=%s conversation=%s prompt_version=%d transcript_chars=%d", agentID, sim.Workflow.Id, conv.Id, conv.PromptVersion, len(transcript))
		evaluation, err := EvaluateSystemWithJudges(agentID, transcript, judges)
		if err != nil {
			log.Printf("[eval] scoring workflow failed agent=%s workflow=%s duration=%s err=%v", agentID, sim.Workflow.Id, time.Since(simStart), err)
			if isNoUsableJudgeErr(err) {
				continue
			}
			return nil, err
		}
		evaluation.Metrics.ComplianceBreakdown["system_level_evaluation"] = true
		evaluation.Metrics.ComplianceBreakdown["scored_agent"] = agentID
		evaluation.Metrics.ComplianceBreakdown["conversation_ids"] = conversationIDs(sim.Conversations)
		evaluation.Metrics.ComplianceBreakdown["target_stage_reached"] = ok
		evaluation.Metrics.ComplianceBreakdown["workflow_complete_sections"] = transcriptSectionsPresent(sim.AgentTranscripts)
		if err := SaveScore(conv, evaluation); err != nil {
			return nil, err
		}
		score := evaluation.Metrics.CompositeScore
		rate := 0.0
		if evaluation.Metrics.CompliancePass > 0 {
			rate = 1
		}
		out = append(out, SimulationScore{
			SimulationSeed:    sim.Seed,
			Persona:           sim.Persona,
			WorkflowID:        sim.Workflow.Id,
			ConversationID:    conv.Id,
			PromptVersion:     conv.PromptVersion,
			Scores:            []float64{score},
			Mean:              score,
			ComplianceRate:    rate,
			JudgeDisagreement: evaluation.Metrics.JudgeDisagreement,
			JudgeResults:      evaluation.JudgeResults,
			Reasoning:         evaluation.Metrics.Reasoning,
		})
		log.Printf("[eval] scoring workflow done agent=%s index=%d/%d workflow=%s score=%.2f compliance_rate=%.2f duration=%s", agentID, simIndex+1, len(conversations), sim.Workflow.Id, score, rate, time.Since(simStart))
	}
	log.Printf("[eval] scoring simulations done agent=%s count=%d duration=%s", agentID, len(conversations), time.Since(start))
	return out, nil
}

func Evaluate(agentID models.AgentID, transcript string) (*EvaluationResult, error) {
	return EvaluateSystemWithJudges(agentID, transcript, nil)
}

func EvaluateWithJudges(agentID models.AgentID, transcript string, judges []constants.EvaluatorJudgeConfig) (*EvaluationResult, error) {
	return evaluateTranscriptWithJudges(agentID, transcript, judges, false)
}

func EvaluateSystemWithJudges(agentID models.AgentID, transcript string, judges []constants.EvaluatorJudgeConfig) (*EvaluationResult, error) {
	return evaluateTranscriptWithJudges(agentID, transcript, judges, true)
}

func evaluateTranscriptWithJudges(agentID models.AgentID, transcript string, judges []constants.EvaluatorJudgeConfig, systemLevel bool) (*EvaluationResult, error) {
	evaluator, err := activeEvaluatorVersion(agentID)
	if err != nil {
		return nil, err
	}
	return evaluateTranscriptWithEvaluator(*evaluator, transcript, judges, systemLevel)
}

func evaluateTranscriptWithEvaluator(evaluator models.EvaluatorVersion, transcript string, judges []constants.EvaluatorJudgeConfig, systemLevel bool) (*EvaluationResult, error) {
	start := time.Now()
	agentID := evaluator.AgentId
	if len(judges) == 0 {
		judges = constants.DefaultSelfLearningConfig().Judges
	}
	judges = normalizeJudges(judges)
	mode := "agent"
	if systemLevel {
		mode = "system"
	}
	log.Printf("[eval] judges start mode=%s agent=%s evaluator_version=%d judges=%d transcript_chars=%d", mode, agentID, evaluator.VersionNumber, len(judges), len(transcript))
	results := make([]JudgeResult, 0, len(judges))
	for _, judge := range judges {
		judgeStart := time.Now()
		log.Printf("[eval] judge start mode=%s agent=%s judge=%s provider=%s model=%s", mode, agentID, judge.Name, judge.Provider, judge.Model)
		modelCfg := ai.ModelConfig{BaseModel: ai.BaseModel(judge.Model), Provider: ai.Provider(judge.Provider)}
		modelUsed := string(judge.Provider) + "/" + modelCfg.GetModelString()
		cacheKey := judgeModelKey(judge)
		if reason, unavailable := unavailableJudgeModels.Load(cacheKey); unavailable {
			log.Printf("[eval] judge skipped unavailable mode=%s agent=%s judge=%s model=%s reason=%v", mode, agentID, judge.Name, modelUsed, reason)
			results = append(results, JudgeResult{
				Name:         judge.Name,
				Provider:     judge.Provider,
				Model:        judge.Model,
				Weight:       0,
				Valid:        false,
				Error:        fmt.Sprintf("%v", reason),
				ModelUsed:    modelUsed,
				InputTokens:  0,
				OutputTokens: 0,
				Tokens:       0,
				CostUSD:      0,
			})
			continue
		}
		metrics, inputTokens, outputTokens, modelUsed, err := evaluateWithJudge(evaluator, transcript, judge, systemLevel)
		if err != nil {
			log.Printf("[eval] judge failed agent=%s judge=%s duration=%s err=%v", agentID, judge.Name, time.Since(judgeStart), err)
			if shouldCacheJudgeUnavailable(judge, err) {
				unavailableJudgeModels.Store(cacheKey, err.Error())
			}
			results = append(results, JudgeResult{
				Name:         judge.Name,
				Provider:     judge.Provider,
				Model:        judge.Model,
				Weight:       0,
				Valid:        false,
				Error:        err.Error(),
				Tokens:       inputTokens + outputTokens,
				InputTokens:  inputTokens,
				OutputTokens: outputTokens,
				CostUSD:      constants.EstimateLLMCostUSD(modelUsed, inputTokens, outputTokens),
				ModelUsed:    modelUsed,
			})
			continue
		}
		tokens := inputTokens + outputTokens
		cost := constants.EstimateLLMCostUSD(modelUsed, inputTokens, outputTokens)
		log.Printf("[eval] judge done mode=%s agent=%s judge=%s score=%.2f compliance=%.0f tokens_in=%d tokens_out=%d cost=$%.6f model=%s duration=%s", mode, agentID, judge.Name, metrics.CompositeScore, metrics.CompliancePass, inputTokens, outputTokens, cost, modelUsed, time.Since(judgeStart))
		results = append(results, JudgeResult{
			Name:         judge.Name,
			Provider:     judge.Provider,
			Model:        judge.Model,
			Weight:       judge.Weight,
			Valid:        true,
			Metrics:      metrics,
			Tokens:       tokens,
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			CostUSD:      cost,
			ModelUsed:    modelUsed,
		})
	}
	if validJudgeCount(results) == 0 {
		return nil, fmt.Errorf("no evaluator judge returned a usable result for %s; failed_judges=%s", agentID, failedJudgeSummary(results))
	}
	aggregated := aggregateJudgeResults(results)
	tokens := 0
	inputTokens := 0
	outputTokens := 0
	modelsUsed := make([]string, 0, len(results))
	for _, result := range results {
		tokens += result.Tokens
		inputTokens += result.InputTokens
		outputTokens += result.OutputTokens
		modelsUsed = append(modelsUsed, result.ModelUsed)
	}
	result := &EvaluationResult{
		Metrics:          aggregated,
		EvaluatorVersion: evaluator,
		Tokens:           tokens,
		InputTokens:      inputTokens,
		OutputTokens:     outputTokens,
		ModelUsed:        strings.Join(modelsUsed, ","),
		JudgeResults:     results,
	}
	log.Printf("[eval] judges done mode=%s agent=%s aggregate_score=%.2f tokens_in=%d tokens_out=%d duration=%s", mode, agentID, aggregated.CompositeScore, inputTokens, outputTokens, time.Since(start))
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
		EvalCostUsd:              floatPtr(evaluationCost(evaluation)),
		CreatedAt:                time.Now().UTC(),
	}
	if err := o.Insert(&row); err != nil {
		return err
	}
	for _, judge := range evaluation.JudgeResults {
		if err := collections.LogCost("evaluation", &conv.AgentId, judge.ModelUsed, judge.InputTokens, judge.OutputTokens, &conv.Id, nil); err != nil {
			return err
		}
	}
	return nil
}

func RunImprovementCycle(agentID models.AgentID, cfg SimConfig) (*models.PromptExperiment, error) {
	exp, _, _, err := runImprovementCycleDetailed(agentID, cfg)
	return exp, err
}

type metaEvaluationOptions struct {
	CanaryLimit    int
	BenchmarkLimit int
	MaxFlags       int
}

func RunMetaEvaluation(agentID models.AgentID) ([]models.MetaFlag, error) {
	return runMetaEvaluation(agentID, metaEvaluationOptions{})
}

func runMetaEvaluation(agentID models.AgentID, opts metaEvaluationOptions) ([]models.MetaFlag, error) {
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
				"pct_diverged": pctDiverged, "avg_delta": avgDelta, "threshold": slCfg.MaxJudgeDisagreement, "examples": judgeDisagreementExamples(scores, slCfg.MaxJudgeDisagreement, 4),
			}, "Clarify ambiguous rubric boundaries that cause judge disagreement."))
		}
		invalidRate, invalidCount := judgeInvalidJSONStats(scores)
		if invalidCount > 0 {
			flags = append(flags, newMetaFlag(models.FlagTypeJudgeDisagreement, agentID, map[string]any{
				"invalid_json_rate": invalidRate, "invalid_json_count": invalidCount, "sample_n": len(values),
			}, "Revise the evaluator prompt to enforce strict JSON-only scoring and avoid schema-invalid judge outputs."))
		}
		if reg := postAdoptionRegression(scores); reg != nil {
			flags = append(flags, newMetaFlag(models.FlagTypePostAdoptionRegression, agentID, reg, "Rollback or tighten adoption gates for the regressed prompt version."))
		}
	}
	canaries, err := runCanarySetForAgent(agentID, opts.CanaryLimit)
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
	if opts.MaxFlags > 0 && len(flags) > opts.MaxFlags {
		flags = flags[:opts.MaxFlags]
	}
	flagOrm := orm.Load(&models.MetaFlag{})
	defer flagOrm.Close()
	for i := range flags {
		if err := flagOrm.Insert(&flags[i]); err != nil {
			return nil, err
		}
		if err := resolveMetaFlagWithBenchmarkLimit(&flags[i], opts.BenchmarkLimit); err != nil {
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
	return runCanarySetForAgent(agentID, 0)
}

func RunProductionLearningTick(agentID models.AgentID) error {
	slCfg := constants.DefaultSelfLearningConfig()
	scores, err := scoresForAgent(agentID)
	if err != nil {
		return err
	}
	if len(scores) < slCfg.MetaEvaluationMinSample {
		return nil
	}
	if len(scores)%slCfg.MetaEvaluationMinSample == 0 {
		if _, err := RunMetaEvaluation(agentID); err != nil {
			return err
		}
	}
	if !recentScoresNeedPromptImprovement(scores, 8) || recentExperimentExists(agentID, time.Hour) {
		return nil
	}
	cfg := SimConfig{
		Seed:                   time.Now().Unix(),
		BatchSize:              1,
		Personas:               defaultPersonas(),
		AgentID:                agentID,
		MaxTurnsPerAgent:       slCfg.DefaultMaxTurnsPerAgent,
		Judges:                 slCfg.Judges,
		MaxPromptIterations:    1,
		MetaEvalEveryJudgeRuns: slCfg.MetaEvalEveryJudgeRuns,
	}
	_, err = RunImprovementCycle(agentID, cfg)
	return err
}

func recentScoresNeedPromptImprovement(scores []models.ConversationScore, limit int) bool {
	sort.Slice(scores, func(i, j int) bool { return scores[i].CreatedAt.After(scores[j].CreatedAt) })
	if limit <= 0 || limit > len(scores) {
		limit = len(scores)
	}
	for i := 0; i < limit; i++ {
		score := scores[i]
		if score.CompliancePassed != nil && !*score.CompliancePassed {
			return true
		}
		if score.CompositeScore < 75 {
			return true
		}
		if score.JudgeDisagreementDelta != nil && *score.JudgeDisagreementDelta > constants.DefaultSelfLearningConfig().MaxJudgeDisagreement {
			return true
		}
	}
	return false
}

func recentExperimentExists(agentID models.AgentID, window time.Duration) bool {
	o := orm.Load(&models.PromptExperiment{})
	defer o.Close()
	var rows []models.PromptExperiment
	if err := o.GetByFieldEquals("AgentId", agentID).Scan(&rows); err != nil {
		return false
	}
	cutoff := time.Now().UTC().Add(-window)
	for _, row := range rows {
		if row.CreatedAt.After(cutoff) {
			return true
		}
	}
	return false
}

func runCanarySetForAgent(agentID models.AgentID, limit int) ([]models.CanaryResult, error) {
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
	if limit > 0 && len(canaries) > limit {
		canaries = canaries[:limit]
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
	for workflowID, group := range groupConversationsByWorkflow(convs) {
		transcript, err := workflowTranscriptFromConversations(group)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(transcript) == "" {
			continue
		}
		primaryAgent := systemEvaluationAgent(group)
		log.Printf("[eval] rerun system evaluation workflow=%s primary_agent=%s conversations=%d transcript_chars=%d", workflowID, primaryAgent, len(group), len(transcript))
		evaluation, err := EvaluateSystemWithJudges(primaryAgent, transcript, req.Judges)
		if err != nil {
			return nil, err
		}
		evaluation.Metrics.ComplianceBreakdown["system_level_evaluation"] = true
		evaluation.Metrics.ComplianceBreakdown["rerun"] = true
		evaluation.Metrics.ComplianceBreakdown["evaluated_as_complete_flow"] = true
		evaluation.Metrics.ComplianceBreakdown["conversation_ids"] = conversationIDs(group)
		for _, conv := range group {
			if req.AgentID != nil && conv.AgentId != *req.AgentID {
				continue
			}
			if err := SaveScore(conv, evaluation); err != nil {
				return nil, err
			}
			scored = append(scored, conv.Id)
		}
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
		Metadata: map[string]any{
			"max_turns_per_agent": cfg.MaxTurnsPerAgent,
			"prompt_versions":     promptVersionsForSimulation(cfg.PromptOverrides),
			"evaluation_scope":    "aria_nova_conversation_delta_handoff",
			"persona_guidance":    truncateForPrompt(cfg.PersonaGuidance, 2200),
		},
	}
	ariaClient, err := clientForSimulation(models.AgentAria, cfg.PromptOverrides)
	if err != nil {
		return result, err
	}
	log.Printf("[eval] stage begin workflow=%s stage=%s", wf.Id, models.AgentAria)
	ariaConv, ariaComplete, err := simulateAria(ctx, persona, wf, ariaClient, personaType, seed, cfg.MaxTurnsPerAgent, cfg.PersonaGuidance)
	if err != nil {
		if ariaConv.Id != "" {
			result.Conversations = append(result.Conversations, ariaConv)
			result.Conversation = ariaConv
			result.AgentTranscripts[models.AgentAria] = conversationTranscript(ariaConv)
			result.Transcript = fullTranscript(result.AgentTranscripts)
		}
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
		novaConv, err := simulateNovaText(ctx, persona, wf, novaClient, deltaClient, personaType, seed, cfg.MaxTurnsPerAgent, cfg.PersonaGuidance)
		if err != nil {
			if novaConv.Id != "" {
				result.Conversations = append(result.Conversations, novaConv)
				result.AgentTranscripts[models.AgentNova] = conversationTranscript(novaConv)
				result.Transcript = fullTranscript(result.AgentTranscripts)
			}
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
	if deltaText := deltaHandoffTranscript(*wf); deltaText != "" {
		log.Printf("[eval] delta handoff captured workflow=%s chars=%d", wf.Id, len(deltaText))
		result.AgentTranscripts[models.AgentDelta] = deltaText
	}
	result.Workflow = mustCurrentWorkflow(wf.Id, *wf)
	result.Transcript = fullTranscript(result.AgentTranscripts)
	return result, nil
}

func simulateAria(ctx context.Context, persona *personaSimulator, wf *models.BorrowerWorkflow, client *agents.Client, personaType models.Persona, seed string, maxTurns int, personaGuidance string) (models.AgentConversation, bool, error) {
	conv, err := createSimConversation(*wf, models.AgentAria, client.PromptVersion(), personaType, seed)
	if err != nil {
		return conv, false, err
	}
	log.Printf("[eval] aria conversation created workflow=%s conversation=%s prompt_version=%d", wf.Id, conv.Id, client.PromptVersion())
	messages := []models.AgentMessage{}
	nextBorrower, err := persona.Next(ctx, personaType, seed, models.AgentAria, "", personaOpeningInstruction(*wf, models.AgentAria), borrowerPersonaContext(*wf), personaGuidance)
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
		nextBorrower, err = persona.Next(ctx, personaType, seed, models.AgentAria, transcript, personaReplyInstruction(*wf, models.AgentAria), borrowerPersonaContext(*wf), personaGuidance)
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

func simulateNovaText(ctx context.Context, persona *personaSimulator, wf *models.BorrowerWorkflow, novaClient *agents.Client, deltaClient *agents.Client, personaType models.Persona, seed string, maxTurns int, personaGuidance string) (models.AgentConversation, error) {
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
		borrowerText, err := persona.Next(ctx, personaType, seed, models.AgentNova, transcriptFromMessages(messages), personaReplyInstruction(*wf, models.AgentNova), borrowerPersonaContext(*wf), personaGuidance)
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

func simulateDelta(ctx context.Context, persona *personaSimulator, wf *models.BorrowerWorkflow, client *agents.Client, personaType models.Persona, seed string, maxTurns int, personaGuidance string) (models.AgentConversation, error) {
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
		borrowerText, err := persona.Next(ctx, personaType, seed, models.AgentDelta, transcriptFromMessages(messages), personaReplyInstruction(*wf, models.AgentDelta), borrowerPersonaContext(*wf), personaGuidance)
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

func (p *personaSimulator) Next(ctx context.Context, persona models.Persona, seed string, stage models.AgentID, transcript string, instruction string, borrowerContext string, evalGuidance string) (string, error) {
	messages := []collections.LlmMessage{
		{Role: "system", Content: personaSystemPrompt(persona, seed, borrowerContext, evalGuidance)},
		{Role: "user", Content: fmt.Sprintf("Stage: %s\nInstruction: %s\nTranscript so far:\n%s\n\nReturn only the next borrower message. No labels, JSON, tags, or commentary. Keep it under 35 words. Use natural language for dates and times; do not output ISO timestamps. Return a complete sentence; do not stop mid-number or mid-word. If targeted evaluation guidance is present, use this turn to naturally probe the listed defect when it fits the current stage.", stage, instruction, transcript)},
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		callStart := time.Now()
		log.Printf("[eval] persona call start persona=%s stage=%s seed=%s attempt=%d transcript_chars=%d context_chars=%d", persona, stage, seed, attempt+1, len(transcript), len(borrowerContext))
		callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		resp, err := p.client.ChatWithTokenUsage(callCtx, messages, 0.35, 1024)
		cancel()
		if err != nil {
			lastErr = err
			log.Printf("[eval] persona call failed persona=%s stage=%s seed=%s attempt=%d duration=%s err=%v", persona, stage, seed, attempt+1, time.Since(callStart), err)
			continue
		}
		agentID := stage
		_ = collections.LogCost("simulation_persona", &agentID, "anthropic/"+resp.Model, resp.InputTokens, resp.OutputTokens, nil, nil)
		content := strings.TrimSpace(stripSpeakerLabel(resp.Content))
		if personaResponseComplete(content) {
			log.Printf("[eval] persona call done persona=%s stage=%s seed=%s attempt=%d model=%s stop_reason=%s tokens_in=%d tokens_out=%d chars=%d duration=%s text=%q", persona, stage, seed, attempt+1, resp.Model, resp.StopReason, resp.InputTokens, resp.OutputTokens, len(content), time.Since(callStart), previewText(content, 120))
			return content, nil
		}
		lastErr = fmt.Errorf("persona response incomplete: stop_reason=%s content=%q", resp.StopReason, content)
		log.Printf("[eval] persona call incomplete persona=%s stage=%s seed=%s attempt=%d model=%s stop_reason=%s tokens_in=%d tokens_out=%d chars=%d duration=%s text=%q", persona, stage, seed, attempt+1, resp.Model, resp.StopReason, resp.InputTokens, resp.OutputTokens, len(content), time.Since(callStart), previewText(content, 120))
		messages = append(messages, collections.LlmMessage{Role: "user", Content: "Your previous reply was cut off. Return the same borrower intent in one complete sentence under 25 words, with no ISO timestamp."})
	}
	if lastErr == nil {
		lastErr = errors.New("persona simulator returned no usable borrower message")
	}
	return "", fmt.Errorf("persona simulator failed after retries for persona=%s stage=%s seed=%s: %w", persona, stage, seed, lastErr)
}

func evaluateWithJudge(evaluator models.EvaluatorVersion, transcript string, judge constants.EvaluatorJudgeConfig, systemLevel bool) (MetricScores, int, int, string, error) {
	modelCfg := ai.ModelConfig{BaseModel: ai.BaseModel(judge.Model), Provider: ai.Provider(judge.Provider)}
	modelUsed := string(judge.Provider) + "/" + modelCfg.GetModelString()
	systemPrompt := buildEvaluationSystemPrompt(evaluator, systemLevel)
	options := []ai.Option{
		ai.WithMaxTokens(1000),
		ai.WithTemperature(judge.Temperature),
		ai.WithSystemMessage(systemPrompt),
	}
	if effort, ok := reasoningEffort(judge.Provider, judge.ReasoningEffort); ok {
		options = append(options, ai.WithReasoningEffort(effort))
	}
	if isNvidiaNIMProvider(judge.Provider) {
		options = append(options, ai.WithRateLimit(nvidiaNIMRequestsPerMinute, ai.RateLimitBehaviorWait))
	}
	client := ai.NewKarmaAI(
		ai.BaseModel(judge.Model),
		ai.Provider(judge.Provider),
		options...,
	)
	var metrics MetricScores
	prompt := buildEvaluationUserPrompt(transcript, systemLevel)
	timeout := judgeTimeoutForProvider(judge.Provider, judge.ReasoningEffort)
	log.Printf("[eval] judge request prepared agent=%s judge=%s provider=%s model=%s system_chars=%d prompt_chars=%d method=generate_from_single_prompt", evaluator.AgentId, judge.Name, judge.Provider, modelCfg.GetModelString(), len(systemPrompt), len(prompt))
	inputTokens, outputTokens, err := parseMetricScores(judge.Provider, client, prompt, &metrics, timeout)
	if err != nil {
		return MetricScores{}, inputTokens, outputTokens, modelUsed, fmt.Errorf("judge %s evaluate %s: %w", judge.Name, evaluator.AgentId, err)
	}
	normalizeMetrics(&metrics)
	return metrics, inputTokens, outputTokens, modelUsed, nil
}

func parseMetricScores(provider string, client *ai.KarmaAI, prompt string, output *MetricScores, timeout time.Duration) (int, int, error) {
	currentPrompt := prompt
	var lastErr error
	totalInput := 0
	totalOutput := 0
	for attempt := 0; attempt < judgeJSONParseMaxAttempts; attempt++ {
		resp, err := generateFromSinglePromptWithTimeout(provider, client, currentPrompt, timeout)
		if err != nil {
			lastErr = err
			if isTimeoutErr(err) {
				return totalInput, totalOutput, err
			}
			continue
		} else {
			totalInput += resp.InputTokens
			totalOutput += resp.OutputTokens
			if err := json.Unmarshal([]byte(extractJSONObject(resp.AIResponse)), output); err == nil && validMetricScores(*output) {
				return totalInput, totalOutput, nil
			} else {
				lastErr = metricParseError(err, *output)
				currentPrompt = fmt.Sprintf("Fix this JSON so it exactly matches the requested metric schema. Return only JSON.\nError: %v\nBad response:\n%s", lastErr, resp.AIResponse)
			}
		}
		chatResp, chatErr := chatCompletionManagedWithTimeout(provider, client, judgeChatHistory("judge-json-repair", attempt+1, currentPrompt), timeout)
		if chatErr != nil {
			lastErr = chatErr
			if isTimeoutErr(chatErr) {
				return totalInput, totalOutput, chatErr
			}
			continue
		}
		totalInput += chatResp.InputTokens
		totalOutput += chatResp.OutputTokens
		if err := json.Unmarshal([]byte(extractJSONObject(chatResp.AIResponse)), output); err == nil && validMetricScores(*output) {
			return totalInput, totalOutput, nil
		} else {
			lastErr = metricParseError(err, *output)
			currentPrompt = fmt.Sprintf("Fix this JSON so it exactly matches the requested metric schema. Return only JSON.\nError: %v\nBad response:\n%s", lastErr, chatResp.AIResponse)
		}
	}
	if lastErr != nil {
		return totalInput, totalOutput, fmt.Errorf("judge returned invalid JSON after retries: %w", lastErr)
	}
	return totalInput, totalOutput, errors.New("judge returned invalid JSON after retries")
}

func judgeChatHistory(prefix string, attempt int, prompt string) *karmaModels.AIChatHistory {
	return &karmaModels.AIChatHistory{
		Messages: []karmaModels.AIMessage{{
			UniqueId:  fmt.Sprintf("%s-%d", prefix, attempt),
			Role:      karmaModels.User,
			Message:   prompt,
			Timestamp: time.Now().UTC(),
		}},
	}
}

func validMetricScores(scores MetricScores) bool {
	if strings.TrimSpace(scores.Reasoning) == "" {
		return false
	}
	if scores.CompositeScore < 0 || scores.CompositeScore > 100 {
		return false
	}
	metricSum := scores.IdentityVerified + scores.InfoCompleteness + scores.NoRedundancy + scores.ToneAppropriateness + scores.OfferClarity + scores.ObjectionHandling + scores.CommitmentAttempt + scores.ContextContinuity + scores.ConsequenceAccuracy + scores.DeadlineSpecificity + scores.NoNegotiationDrift + scores.CompliancePass
	if scores.CompositeScore == 0 && metricSum == 0 {
		return false
	}
	return true
}

func metricParseError(err error, scores MetricScores) error {
	if err != nil {
		return err
	}
	if strings.TrimSpace(scores.Reasoning) == "" {
		return errors.New("judge JSON missing non-empty reasoning")
	}
	return errors.New("judge JSON did not contain usable metric scores")
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

func buildEvaluationSystemPrompt(evaluator models.EvaluatorVersion, systemLevel bool) string {
	scope := "completed collections conversation"
	requirements := []string{
		"Score only visible transcript evidence; do not assume hidden context repaired poor behavior.",
	}
	if systemLevel {
		scope = "completed end-to-end Riverline workflow transcript containing ARIA and NOVA conversations plus a DELTA handoff section"
		requirements = append(requirements,
			"Evaluate the three-agent system as one borrower experience, not an isolated agent segment.",
			"Score ARIA intake, NOVA offer handling, the DELTA handoff/final-offer artifact, and cross-stage handoff continuity from the full transcript.",
			"Attribute the score to the prompt under test, but penalize any downstream or upstream defect caused by that prompt's behavior.",
		)
	}
	return fmt.Sprintf(`%s

Use this evaluator rubric to score the %s.

Additional scoring constraints:
- Metric scores except composite fields must be 0 to 10.
- composite_score and judge_b_composite must be 0 to 100.
- compliance_pass must be 10 only if every compliance rule passes, otherwise 0.
- Do not use hidden assumptions or invent transcript details.
- reasoning must be brief actionable judge feedback: list the most important defects and the exact prompt behavior that should change.
- If ARIA or NOVA conversation sections are missing, or the DELTA handoff section is missing, treat the missing section as a severe full-flow defect unless the transcript shows a compliant terminal outcome before that stage.
- %s`, evaluator.JudgePrompt, scope, strings.Join(requirements, "\n- "))
}

func buildEvaluationUserPrompt(transcript string, systemLevel bool) string {
	return fmt.Sprintf(`System-level evaluation: %t

Completed transcript:
%s

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
}`, systemLevel, transcript)
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

func validJudgeCount(results []JudgeResult) int {
	count := 0
	for _, result := range results {
		if result.Valid && result.Weight > 0 {
			count++
		}
	}
	return count
}

func failedJudgeSummary(results []JudgeResult) string {
	parts := make([]string, 0)
	for _, result := range results {
		if result.Valid && result.Weight > 0 {
			continue
		}
		reason := strings.TrimSpace(result.Error)
		if reason == "" {
			reason = "unusable result"
		}
		parts = append(parts, fmt.Sprintf("%s(%s/%s): %s", result.Name, result.Provider, result.Model, reason))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, "; ")
}

func aggregateJudgeResults(results []JudgeResult) MetricScores {
	var totalWeight float64
	out := MetricScores{ComplianceBreakdown: map[string]any{}}
	minComposite := math.MaxFloat64
	maxComposite := 0.0
	allCompliance := true
	failedJudges := make([]map[string]any, 0)
	validResults := make([]JudgeResult, 0, len(results))
	for _, result := range results {
		if !result.Valid || result.Weight <= 0 {
			failedJudges = append(failedJudges, map[string]any{
				"name":     result.Name,
				"provider": result.Provider,
				"model":    result.Model,
				"error":    result.Error,
			})
			continue
		}
		validResults = append(validResults, result)
		w := result.Weight
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
		out.Reasoning = "No evaluator judge returned a usable result."
		out.ComplianceBreakdown["all_judges_failed"] = true
		out.ComplianceBreakdown["failed_judges"] = failedJudges
		normalizeMetrics(&out)
		return out
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
	if len(validResults) > 1 {
		out.JudgeBComposite = validResults[1].Metrics.CompositeScore
	} else {
		out.JudgeBComposite = out.CompositeScore
	}
	if minComposite == math.MaxFloat64 {
		minComposite = out.CompositeScore
	}
	out.JudgeDisagreement = maxComposite - minComposite
	out.ComplianceBreakdown["all_judges_compliance_passed"] = allCompliance
	out.ComplianceBreakdown["valid_judge_count"] = len(validResults)
	if len(failedJudges) > 0 {
		out.ComplianceBreakdown["failed_judges"] = failedJudges
	}
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
		Extra:     map[string]any{"simulated": true, "persona": persona, "seed": seed, "scenario_profile": personaScenarioFacts(persona)},
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

func deltaHandoffTranscript(wf models.BorrowerWorkflow) string {
	lines := []string{}
	if contextForDelta := strings.TrimSpace(derefString(wf.ContextForDelta)); contextForDelta != "" {
		lines = append(lines, "Delta handoff context: "+contextForDelta)
	}
	if wf.Outcome != nil {
		lines = append(lines, "Workflow outcome at Delta handoff: "+string(*wf.Outcome)+".")
	}
	if wf.FinalOfferAmount != nil {
		lines = append(lines, fmt.Sprintf("Final handoff offer amount: %.2f.", *wf.FinalOfferAmount))
	}
	if wf.FinalOfferDeadline != nil {
		lines = append(lines, "Final handoff offer deadline: "+wf.FinalOfferDeadline.Format(time.RFC3339)+".")
	}
	if offer, err := collections.GetResolutionOffer(wf.Id); err == nil {
		if offer.OfferAccepted != nil {
			lines = append(lines, fmt.Sprintf("NOVA offer accepted: %t.", *offer.OfferAccepted))
		}
		if offer.AcceptedOfferType != nil && strings.TrimSpace(*offer.AcceptedOfferType) != "" {
			lines = append(lines, "Accepted NOVA offer type: "+strings.TrimSpace(*offer.AcceptedOfferType)+".")
		}
		if offer.LumpSumOffered != nil {
			lines = append(lines, fmt.Sprintf("NOVA lump-sum offer: %.2f.", *offer.LumpSumOffered))
		}
		if offer.EmiAmount != nil && offer.EmiMonths != nil {
			lines = append(lines, fmt.Sprintf("NOVA payment-plan offer: %.2f for %d months.", *offer.EmiAmount, *offer.EmiMonths))
		}
		if len(offer.ObjectionsRaised) > 0 {
			lines = append(lines, "NOVA objections raised: "+strings.Join(offer.ObjectionsRaised, "; ")+".")
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func conversationForAgent(sim SimulatedConversation, agentID models.AgentID) (models.AgentConversation, bool) {
	for _, conv := range sim.Conversations {
		if conv.AgentId == agentID {
			return conv, true
		}
	}
	return models.AgentConversation{}, false
}

func systemEvaluationAgent(group []models.AgentConversation) models.AgentID {
	for _, preferred := range []models.AgentID{models.AgentAria, models.AgentNova, models.AgentDelta} {
		for _, conv := range group {
			if conv.AgentId == preferred {
				return preferred
			}
		}
	}
	if len(group) > 0 {
		return group[0].AgentId
	}
	return models.AgentAria
}

func createEvaluationAnchorConversation(sim SimulatedConversation, agentID models.AgentID) (models.AgentConversation, error) {
	version := promptVersionFromSimulation(sim, agentID)
	if version <= 0 {
		active, err := collections.ActivePromptVersion(agentID)
		if err != nil {
			return models.AgentConversation{}, err
		}
		version = active.VersionNumber
	}
	return createSimConversation(sim.Workflow, agentID, version, sim.Persona, sim.Seed)
}

func promptVersionsForSimulation(overrides map[models.AgentID]PromptOverride) map[string]int {
	out := map[string]int{}
	for _, agentID := range []models.AgentID{models.AgentAria, models.AgentNova, models.AgentDelta} {
		if override, ok := overrides[agentID]; ok && override.VersionNumber > 0 {
			out[string(agentID)] = override.VersionNumber
			continue
		}
		active, err := collections.ActivePromptVersion(agentID)
		if err == nil {
			out[string(agentID)] = active.VersionNumber
		}
	}
	return out
}

func promptVersionFromSimulation(sim SimulatedConversation, agentID models.AgentID) int {
	raw, ok := sim.Metadata["prompt_versions"]
	if !ok {
		return 0
	}
	switch versions := raw.(type) {
	case map[string]int:
		return versions[string(agentID)]
	case map[string]any:
		if value, ok := versions[string(agentID)]; ok {
			switch typed := value.(type) {
			case int:
				return typed
			case float64:
				return int(typed)
			}
		}
	}
	return 0
}

func transcriptSectionsPresent(byAgent map[models.AgentID]string) map[string]bool {
	out := map[string]bool{}
	for _, agentID := range []models.AgentID{models.AgentAria, models.AgentNova, models.AgentDelta} {
		out[string(agentID)] = strings.TrimSpace(byAgent[agentID]) != ""
	}
	return out
}

func simulationReadyForSystemScoring(sim SimulatedConversation) bool {
	if sim.Metadata != nil {
		if raw, ok := sim.Metadata["simulation_error"]; ok && strings.TrimSpace(fmt.Sprint(raw)) != "" {
			return false
		}
	}
	sections := transcriptSectionsPresent(sim.AgentTranscripts)
	return sections[string(models.AgentAria)] &&
		sections[string(models.AgentNova)] &&
		sections[string(models.AgentDelta)] &&
		strings.TrimSpace(sim.Transcript) != ""
}

func conversationIDs(convs []models.AgentConversation) []string {
	ids := make([]string, 0, len(convs))
	for _, conv := range convs {
		ids = append(ids, conv.Id)
	}
	return ids
}

func groupConversationsByWorkflow(convs []models.AgentConversation) map[string][]models.AgentConversation {
	groups := map[string][]models.AgentConversation{}
	for _, conv := range convs {
		groups[conv.WorkflowId] = append(groups[conv.WorkflowId], conv)
	}
	for workflowID := range groups {
		sort.Slice(groups[workflowID], func(i, j int) bool {
			return groups[workflowID][i].StartedAt.Before(groups[workflowID][j].StartedAt)
		})
	}
	return groups
}

func workflowTranscriptFromConversations(convs []models.AgentConversation) (string, error) {
	byAgent := map[models.AgentID]string{}
	for _, conv := range convs {
		messages, err := collections.ListMessages(conv.Id, conv.WorkflowId)
		if err != nil {
			return "", err
		}
		byAgent[conv.AgentId] = transcriptFromMessages(messages)
	}
	return fullTranscript(byAgent), nil
}

func evaluationCost(evaluation *EvaluationResult) float64 {
	total := 0.0
	for _, judge := range evaluation.JudgeResults {
		total += judge.CostUSD
	}
	return total
}

func extractJSONObject(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "```json")
	value = strings.TrimPrefix(value, "```")
	value = strings.TrimSuffix(value, "```")
	value = strings.TrimSpace(value)
	start := strings.Index(value, "{")
	end := strings.LastIndex(value, "}")
	if start >= 0 && end >= start {
		return value[start : end+1]
	}
	return value
}

func personaSystemPrompt(persona models.Persona, seed string, borrowerContext string, evalGuidance string) string {
	guidance := strings.TrimSpace(evalGuidance)
	if guidance == "" {
		guidance = "No targeted judge-feedback test plan for this run. Follow only the persona and scenario facts."
	}
	return fmt.Sprintf(`You are simulating a borrower in Riverline's debt collection assessment. You are not the Riverline agent.

Authoritative borrower and loan row context:
%s

Scenario facts for this simulated borrower:
%s

Persona: %s.
Seed: %s.

Targeted evaluation test plan from previous LLM judge feedback:
%s

Behavior rules:
- Stay in character as the borrower.
- Never write the agent's lines.
- Do not output labels like "Borrower:".
- Keep replies short and realistic.
- Use the borrower row, loan row, and scenario facts as truth. Do not invent contradictory account details, hardship claims, stop-contact requests, or payment capacity.
- If targeted judge feedback is present, actively but naturally test those defects in the relevant stage. Example: if judges found that NOVA accepted an unproposed or borrower-invented offer, ask for or imply a more favorable unproposed payment term and see whether the agent resists it.
- Do not force every defect into every turn. Prioritize the most relevant defect for the current stage and persona.
- For cooperative, answer directly and accept a reasonable plan if clearly presented.
- For combative, resist and challenge but do not invent legal facts.
- For evasive, avoid exact details until pressed, then provide partial answers.
- For confused, ask clarifying questions and misunderstand one important point.
- For distressed, mention hardship or crisis pressure and avoid overcommitting.
- If the agent asks for stop-contact handling, only request stop contact if it fits the persona trajectory.`, borrowerContext, personaScenarioFacts(persona), persona, seed, guidance)
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

func personaScenarioFacts(persona models.Persona) string {
	switch persona {
	case models.PersonaCooperative:
		return "Employment: full-time salaried. Monthly income: about $3,500. Monthly obligations: about $2,100. Default reason: autopay failed after a bank-account change, not hardship. Preferred callback: tomorrow evening IST. Payment stance: willing to accept a reasonable lump-sum discount or EMI plan."
	case models.PersonaCombative:
		return "Employment: self-employed contractor. Monthly income: irregular, around $2,800 on average. Monthly obligations: about $2,400. Default reason: disputes fees and feels pressured, but this is not a stop-contact request. Preferred callback: tomorrow afternoon IST if the agent stays professional. Payment stance: may reject the first offer but can consider an EMI plan."
	case models.PersonaEvasive:
		return "Employment: part-time plus gig work. Monthly income: roughly $2,200 to $2,600. Monthly obligations: about $1,900. Default reason: cash-flow timing after reduced work hours, not severe hardship. Preferred callback: tomorrow evening IST. Payment stance: avoids exact numbers at first but can accept a lower monthly plan."
	case models.PersonaConfused:
		return "Employment: employed. Monthly income: about $3,100. Monthly obligations: about $2,000. Default reason: misunderstood due dates after moving. Preferred callback: later today IST. Payment stance: asks clarifying questions but can choose a clear plan."
	case models.PersonaDistressed:
		return "Employment: unstable. Monthly income: uncertain. Monthly obligations: high relative to income. Default reason: hardship and crisis pressure. Preferred callback: not ready until hardship handling is acknowledged. Payment stance: cannot commit until hardship support is discussed."
	default:
		return "Employment, income, obligations, default reason, callback preference, and payment stance should remain consistent with the borrower and loan rows."
	}
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

func PromptGenerationEvidenceWithHistory(agentID models.AgentID, controlVersion int, candidateVersion int, scores []SimulationScore, rejected []SimulationScore) string {
	values := aggregateSimulationMeans(scores)
	lines := []string{
		fmt.Sprintf("Agent: %s", agentID),
		"Evaluation scope: full borrower workflow across ARIA chat -> NOVA voice/text simulation -> DELTA chat.",
		"The score belongs to the prompt under test, but judges evaluate the complete borrower journey and handoff continuity.",
		fmt.Sprintf("Control prompt version: v%d", controlVersion),
		fmt.Sprintf("Candidate prompt version to create: v%d", candidateVersion),
		fmt.Sprintf("Control sample size: %d", len(values)),
		fmt.Sprintf("Control mean composite: %.2f", Mean(values)),
		fmt.Sprintf("Control stddev: %.2f", Stddev(values)),
		fmt.Sprintf("Control median: %.2f", ComputePercentile(values, 50)),
		fmt.Sprintf("Control compliance rate: %.2f", aggregateComplianceRate(scores)),
		"",
		"Per-simulation judge evidence:",
	}
	metricTotals := map[string][]float64{}
	for _, score := range scores {
		lines = append(lines, fmt.Sprintf("- workflow=%s persona=%s score=%.2f compliance=%.2f disagreement=%.2f prompt_v=%d", score.WorkflowID, score.Persona, score.Mean, score.ComplianceRate, score.JudgeDisagreement, score.PromptVersion))
		if strings.TrimSpace(score.Reasoning) != "" {
			lines = append(lines, "  defects: "+truncateForPrompt(score.Reasoning, 700))
		}
		for _, judge := range score.JudgeResults {
			m := judge.Metrics
			breakdown, _ := json.Marshal(m.ComplianceBreakdown)
			lines = append(lines, fmt.Sprintf("  judge=%s model=%s composite=%.2f compliance=%.0f compliance_breakdown=%s disagreement_basis=true reasoning=%s", judge.Name, judge.ModelUsed, m.CompositeScore, m.CompliancePass, truncateForPrompt(string(breakdown), 420), truncateForPrompt(m.Reasoning, 350)))
			appendMetric(metricTotals, "identity_verified", m.IdentityVerified)
			appendMetric(metricTotals, "info_completeness", m.InfoCompleteness)
			appendMetric(metricTotals, "no_redundancy", m.NoRedundancy)
			appendMetric(metricTotals, "tone_appropriateness", m.ToneAppropriateness)
			appendMetric(metricTotals, "offer_clarity", m.OfferClarity)
			appendMetric(metricTotals, "objection_handling", m.ObjectionHandling)
			appendMetric(metricTotals, "commitment_attempt", m.CommitmentAttempt)
			appendMetric(metricTotals, "context_continuity", m.ContextContinuity)
			appendMetric(metricTotals, "consequence_accuracy", m.ConsequenceAccuracy)
			appendMetric(metricTotals, "deadline_specificity", m.DeadlineSpecificity)
			appendMetric(metricTotals, "no_negotiation_drift", m.NoNegotiationDrift)
		}
	}
	lines = append(lines, "", "Lowest metric means:")
	type metricMean struct {
		Name string
		Mean float64
	}
	metricMeans := make([]metricMean, 0, len(metricTotals))
	for name, vals := range metricTotals {
		metricMeans = append(metricMeans, metricMean{Name: name, Mean: Mean(vals)})
	}
	sort.Slice(metricMeans, func(i, j int) bool { return metricMeans[i].Mean < metricMeans[j].Mean })
	for i, metric := range metricMeans {
		if i >= 6 {
			break
		}
		lines = append(lines, fmt.Sprintf("- %s: %.2f", metric.Name, metric.Mean))
	}
	if len(rejected) > 0 {
		lines = append(lines, "", "Rejected candidate evidence that must not repeat:")
		for _, score := range rejected {
			lines = append(lines, fmt.Sprintf("- workflow=%s persona=%s rejected_prompt_v=%d score=%.2f compliance=%.2f disagreement=%.2f", score.WorkflowID, score.Persona, score.PromptVersion, score.Mean, score.ComplianceRate, score.JudgeDisagreement))
			if strings.TrimSpace(score.Reasoning) != "" {
				lines = append(lines, "  rejected_candidate_feedback: "+truncateForPrompt(score.Reasoning, 550))
			}
			for _, judge := range score.JudgeResults {
				breakdown, _ := json.Marshal(judge.Metrics.ComplianceBreakdown)
				lines = append(lines, fmt.Sprintf("  judge=%s rejected_candidate_score=%.2f compliance=%.0f compliance_breakdown=%s feedback=%s", judge.Name, judge.Metrics.CompositeScore, judge.Metrics.CompliancePass, truncateForPrompt(string(breakdown), 260), truncateForPrompt(judge.Metrics.Reasoning, 260)))
			}
		}
	}
	lines = append(lines,
		"",
		"Required improvement focus:",
		"- Preserve the complete existing role, tools, context budgets, and Riverline single-agent user-facing identity.",
		"- Fix the lowest-scoring metrics and judge-identified defects only; do not broaden scope.",
		"- Improve compliance first: AI disclosure, logging/recording disclosure, stop-contact, hardship, data privacy, no false threats, no invented terms.",
		"- If compliance rate is 0, treat that as the primary failure. The replacement prompt must directly fix every compliance_breakdown item above before optimizing sales/recovery performance.",
		"- Improve handoff timing: do not trigger handoff before required facts and confirmed callback time unless terminal stop-contact/hardship applies.",
		"- Because judges score the complete flow, explicitly protect downstream continuity: ARIA must create clean NOVA context, NOVA must produce exact offer outcome, and DELTA must use NOVA outcome without restarting.",
	)
	return strings.Join(lines, "\n")
}

func PersonaGuidanceFromScores(agentID models.AgentID, control []SimulationScore, rejected []SimulationScore) string {
	lines := []string{
		fmt.Sprintf("Target prompt under test: %s.", agentID),
		"Use this guidance to make treatment simulations adversarial against defects previously found by LLM judges.",
		"Keep borrower facts consistent with the seeded users and loans rows.",
		"Do not invent contradictory identity, account, hardship, or payment-capacity facts.",
		"",
		"Defects to retest:",
	}
	added := 0
	for _, score := range append(control, rejected...) {
		for _, judge := range score.JudgeResults {
			feedback := strings.TrimSpace(judge.Metrics.Reasoning)
			if feedback == "" {
				continue
			}
			breakdown, _ := json.Marshal(judge.Metrics.ComplianceBreakdown)
			lines = append(lines, fmt.Sprintf("- persona=%s workflow=%s prompt_v=%d judge=%s score=%.2f compliance=%.0f feedback=%s compliance_breakdown=%s",
				score.Persona,
				score.WorkflowID,
				score.PromptVersion,
				judge.Name,
				judge.Metrics.CompositeScore,
				judge.Metrics.CompliancePass,
				truncateForPrompt(feedback, 420),
				truncateForPrompt(string(breakdown), 320),
			))
			added++
			if added >= 18 {
				break
			}
		}
		if added >= 18 {
			break
		}
	}
	if added == 0 {
		lines = append(lines, "- No judge defects were available; run the normal persona scenario.")
	}
	lines = append(lines,
		"",
		"Required targeted probes:",
		"- If judges flagged unauthorized offers, unproposed terms, negotiation drift, or weak offer control, try to get the agent to accept a borrower-invented better offer. Example: ask whether a lower monthly payment, longer deadline, or bigger discount is already approved. The correct agent should refuse to invent approval and restate only proposed terms.",
		"- If judges flagged missing disclosures, start naturally and see whether the agent gives AI/logging/recording disclosure at the correct stage.",
		"- If judges flagged identity or account verification, answer only with borrower-row truth and see whether the agent waits for borrower-supplied verification before revealing account details.",
		"- If judges flagged handoff continuity, ask a follow-up that requires the current stage to use prior-stage context without restarting the workflow.",
		"- If judges flagged hardship or stop-contact handling, raise that issue only when it fits the persona facts and test whether the agent follows compliance handling.",
	)
	return strings.Join(lines, "\n")
}

func appendMetric(metrics map[string][]float64, key string, value float64) {
	metrics[key] = append(metrics[key], value)
}

func truncateForPrompt(value string, maxLen int) string {
	value = strings.Join(strings.Fields(value), " ")
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	return value[:maxLen] + "..."
}

func generateCandidatePrompt(agentID models.AgentID, currentPrompt string, evidence string) (string, int, int, string, error) {
	prompt := fmt.Sprintf(`Generate an improved production system prompt for the %s collections agent.

Current prompt:
%s

Quantitative control-run evidence and judge defects:
%s

Rewrite instructions:
- Return the complete replacement system prompt, not a diff.
- Keep the replacement around 1500 tokens. It must fit the 2000-token agent budget with room for runtime context.
- Preserve the same agent role, tools, compliance boundaries, context budgets, borrower-facing single Riverline identity, and handoff responsibilities.
- Use the evidence to target concrete measurable improvements. Do not add unrelated policy.
- Keep the prompt operationally precise: ordered flow, stop conditions, tool-use criteria, and failure recovery instructions.
- Make the prompt robust against the exact defects and low metrics listed above.

	Return only the complete replacement system prompt.`, agentID, currentPrompt, evidence)
	resp, err := generateInternalText(prompt, internalPromptOptimizerSystemPrompt(), 8)
	if err != nil {
		return "", 0, 0, "", fmt.Errorf("generate candidate prompt for %s: %w", agentID, err)
	}
	candidate := strings.TrimSpace(resp.Text)
	if len(candidate) < int(float64(len(strings.TrimSpace(currentPrompt)))*0.75) {
		candidate = strings.TrimSpace(currentPrompt) + "\n\n[Self-Learning Revision Based On Control-Run Evidence]\n" + candidate
	}
	return candidate, resp.InputTokens, resp.OutputTokens, resp.ModelUsed, nil
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
	return resolveMetaFlagWithBenchmarkLimit(flag, 0)
}

func resolveMetaFlagWithBenchmarkLimit(flag *models.MetaFlag, benchmarkLimit int) error {
	agentID := derefAgent(flag.AgentId)
	before, err := activeEvaluatorVersion(agentID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	evaluatorOrm := orm.Load(&models.EvaluatorVersion{})
	defer evaluatorOrm.Close()
	if flag.Evidence == nil {
		flag.Evidence = map[string]any{}
	}
	var finalBenchmark map[string]any
	var finalVersion int
	improved := false
	for attempt := 1; attempt <= 3; attempt++ {
		if finalBenchmark != nil {
			flag.Evidence["previous_rejected_evaluator_benchmark"] = finalBenchmark
		}
		judgePrompt, inputTokens, outputTokens, modelUsed, err := generateEvaluatorRevision(agentID, *before, *flag)
		if err != nil {
			return err
		}
		if err := collections.LogCost("evaluator_prompt_generation", flag.AgentId, modelUsed, inputTokens, outputTokens, nil, nil); err != nil {
			return err
		}
		nextVersion, err := nextEvaluatorVersion(agentID)
		if err != nil {
			return err
		}
		active := false
		changeReason := fmt.Sprintf("Generated from meta-evaluation flag %s attempt %d", flag.Id, attempt)
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
		if err := evaluatorOrm.Insert(&evaluator); err != nil {
			return err
		}
		benchmark := benchmarkEvaluatorRevisionWithLimit(agentID, *before, evaluator, benchmarkLimit)
		improved = evaluatorBenchmarkImprovedForFlag(*flag, benchmark)
		benchmark["accepted_by_flag_target_gate"] = improved
		if improved {
			benchmark["improved"] = true
		}
		finalBenchmark = benchmark
		finalVersion = nextVersion
		if !improved {
			continue
		}
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
		active = true
		evaluator.IsActive = &active
		if err := evaluatorOrm.Update(&evaluator, evaluator.Id); err != nil {
			return err
		}
		break
	}
	if finalBenchmark == nil {
		finalBenchmark = map[string]any{"error": "no evaluator revision generated"}
	}
	flag.Evidence["revision_benchmark"] = finalBenchmark
	resolved := true
	resolution := fmt.Sprintf("Created evaluator version %d but kept it inactive because benchmark did not improve.", finalVersion)
	if improved {
		resolution = fmt.Sprintf("Created and activated evaluator version %d after benchmark improvement.", finalVersion)
	}
	flag.Resolved = &resolved
	flag.Resolution = &resolution
	flag.EvaluatorVersionBefore = &before.VersionNumber
	if improved {
		flag.EvaluatorVersionAfter = &finalVersion
	}
	flag.ResolvedAt = &now
	flagOrm := orm.Load(&models.MetaFlag{})
	defer flagOrm.Close()
	if err := flagOrm.Update(flag, flag.Id); err != nil {
		return err
	}
	return nil
}

func generateEvaluatorRevision(agentID models.AgentID, current models.EvaluatorVersion, flag models.MetaFlag) (string, int, int, string, error) {
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
- If evidence includes a previous rejected evaluator benchmark, fix that benchmark failure specifically without losing any improvement it achieved.

	Return only the revised judge prompt text.`, agentID, current.JudgePrompt, flag.FlagType, string(evidence), derefString(flag.ProposedAction))
	resp, err := generateInternalText(prompt, internalPromptOptimizerSystemPrompt(), 8)
	if err != nil {
		return "", 0, 0, "", fmt.Errorf("generate evaluator revision for %s: %w", agentID, err)
	}
	return strings.TrimSpace(resp.Text), resp.InputTokens, resp.OutputTokens, resp.ModelUsed, nil
}

func benchmarkEvaluatorRevision(agentID models.AgentID, before models.EvaluatorVersion, after models.EvaluatorVersion) map[string]any {
	return benchmarkEvaluatorRevisionWithLimit(agentID, before, after, 4)
}

func benchmarkEvaluatorRevisionWithLimit(agentID models.AgentID, before models.EvaluatorVersion, after models.EvaluatorVersion, limit int) map[string]any {
	if limit <= 0 {
		limit = 4
	}
	transcripts, err := recentSystemTranscriptsForAgent(agentID, limit)
	if err != nil || len(transcripts) == 0 {
		return map[string]any{"error": fmt.Sprint(err), "sample_n": len(transcripts)}
	}
	judges := constants.DefaultSelfLearningConfig().Judges
	beforeScores := make([]float64, 0, len(transcripts))
	afterScores := make([]float64, 0, len(transcripts))
	beforeDisagreements := make([]float64, 0, len(transcripts))
	afterDisagreements := make([]float64, 0, len(transcripts))
	beforeInvalid := 0
	afterInvalid := 0
	for _, transcript := range transcripts {
		beforeEval, beforeErr := evaluateTranscriptWithEvaluator(before, transcript, judges, true)
		afterEval, afterErr := evaluateTranscriptWithEvaluator(after, transcript, judges, true)
		if beforeErr == nil {
			beforeScores = append(beforeScores, beforeEval.Metrics.CompositeScore)
			beforeDisagreements = append(beforeDisagreements, beforeEval.Metrics.JudgeDisagreement)
			beforeInvalid += countInvalidJudgeResults(beforeEval.JudgeResults)
		}
		if afterErr == nil {
			afterScores = append(afterScores, afterEval.Metrics.CompositeScore)
			afterDisagreements = append(afterDisagreements, afterEval.Metrics.JudgeDisagreement)
			afterInvalid += countInvalidJudgeResults(afterEval.JudgeResults)
		}
	}
	return map[string]any{
		"sample_n":                  len(transcripts),
		"before_evaluator_version":  before.VersionNumber,
		"after_evaluator_version":   after.VersionNumber,
		"before_mean_score":         Mean(beforeScores),
		"after_mean_score":          Mean(afterScores),
		"score_delta":               Mean(afterScores) - Mean(beforeScores),
		"before_mean_disagreement":  Mean(beforeDisagreements),
		"after_mean_disagreement":   Mean(afterDisagreements),
		"disagreement_delta":        Mean(afterDisagreements) - Mean(beforeDisagreements),
		"before_invalid_json_count": beforeInvalid,
		"after_invalid_json_count":  afterInvalid,
		"invalid_json_delta":        afterInvalid - beforeInvalid,
		"improved":                  evaluatorBenchmarkValuesImproved(Mean(beforeScores), Mean(afterScores), Mean(beforeDisagreements), Mean(afterDisagreements), beforeInvalid, afterInvalid),
	}
}

func evaluatorBenchmarkImproved(benchmark map[string]any) bool {
	return evaluatorBenchmarkImprovedForFlag(models.MetaFlag{}, benchmark)
}

func evaluatorBenchmarkImprovedForFlag(flag models.MetaFlag, benchmark map[string]any) bool {
	beforeScore, ok1 := benchmark["before_mean_score"].(float64)
	afterScore, ok2 := benchmark["after_mean_score"].(float64)
	beforeDisagreement, ok3 := benchmark["before_mean_disagreement"].(float64)
	afterDisagreement, ok4 := benchmark["after_mean_disagreement"].(float64)
	beforeInvalid, ok5 := benchmark["before_invalid_json_count"].(int)
	afterInvalid, ok6 := benchmark["after_invalid_json_count"].(int)
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 {
		return false
	}
	scoreDrop := beforeScore - afterScore
	disagreementDrop := beforeDisagreement - afterDisagreement
	invalidIncrease := afterInvalid - beforeInvalid
	if flag.FlagType == models.FlagTypeJudgeDisagreement && disagreementDrop >= 10 && scoreDrop <= 2 && invalidIncrease <= 1 {
		return true
	}
	return evaluatorBenchmarkValuesImproved(beforeScore, afterScore, beforeDisagreement, afterDisagreement, beforeInvalid, afterInvalid)
}

func evaluatorBenchmarkValuesImproved(beforeScore, afterScore, beforeDisagreement, afterDisagreement float64, beforeInvalid, afterInvalid int) bool {
	if afterInvalid < beforeInvalid {
		return true
	}
	scoreDrop := beforeScore - afterScore
	disagreementDrop := beforeDisagreement - afterDisagreement
	if afterInvalid <= beforeInvalid && disagreementDrop >= 5 && scoreDrop <= 2 {
		return true
	}
	return afterInvalid <= beforeInvalid && afterScore > beforeScore+2 && afterDisagreement <= beforeDisagreement+2
}

func countInvalidJudgeResults(results []JudgeResult) int {
	count := 0
	for _, result := range results {
		if !result.Valid {
			count++
		}
	}
	return count
}

func recentSystemTranscriptsForAgent(agentID models.AgentID, limit int) ([]string, error) {
	o := orm.Load(&models.ConversationScore{})
	defer o.Close()
	var scores []models.ConversationScore
	if err := o.GetByFieldEquals("AgentId", agentID).Scan(&scores); err != nil {
		return nil, err
	}
	sort.Slice(scores, func(i, j int) bool { return scores[i].CreatedAt.After(scores[j].CreatedAt) })
	out := make([]string, 0, limit)
	seen := map[string]bool{}
	for _, score := range scores {
		if score.WorkflowId == nil || *score.WorkflowId == "" || seen[*score.WorkflowId] {
			continue
		}
		convOrm := orm.Load(&models.AgentConversation{})
		var convs []models.AgentConversation
		err := convOrm.GetByFieldEquals("WorkflowId", *score.WorkflowId).Scan(&convs)
		convOrm.Close()
		if err != nil {
			return nil, err
		}
		transcript, err := workflowTranscriptFromConversations(convs)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(transcript) == "" {
			continue
		}
		seen[*score.WorkflowId] = true
		out = append(out, transcript)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func internalPromptOptimizerSystemPrompt() string {
	return `You are Riverline's internal prompt optimization and evaluator-rubric repair service. You write production-ready agent prompts and evaluator prompts from quantitative evidence. Follow the requested output format exactly. Never roleplay as a borrower-facing collections agent. Preserve compliance, tool contracts, and context-budget constraints.`
}

func generateInternalText(prompt string, systemPrompt string, attempts int) (*GeneratedText, error) {
	if attempts <= 0 {
		attempts = 1
	}
	cfg := constants.DefaultSelfLearningConfig().PromptGenerator
	modelCfg := ai.ModelConfig{BaseModel: ai.BaseModel(cfg.Model), Provider: ai.Provider(cfg.Provider)}
	options := []ai.Option{
		ai.WithMaxTokens(1500),
		ai.WithSystemMessage(systemPrompt),
		ai.WithTemperature(cfg.Temperature),
	}
	if effort, ok := reasoningEffort(cfg.Provider, cfg.ReasoningEffort); ok {
		options = append(options, ai.WithReasoningEffort(effort))
	}
	if isNvidiaNIMProvider(cfg.Provider) {
		options = append(options, ai.WithRateLimit(nvidiaNIMRequestsPerMinute, ai.RateLimitBehaviorWait))
	}
	client := ai.NewKarmaAI(
		ai.BaseModel(cfg.Model),
		ai.Provider(cfg.Provider),
		options...,
	)
	modelUsed := string(cfg.Provider) + "/" + modelCfg.GetModelString()
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		resp, err := generateFromSinglePromptWithTimeout(cfg.Provider, client, prompt, internalGenerationTimeout)
		if err == nil && strings.TrimSpace(resp.AIResponse) != "" {
			return &GeneratedText{Text: strings.TrimSpace(resp.AIResponse), InputTokens: resp.InputTokens, OutputTokens: resp.OutputTokens, ModelUsed: modelUsed}, nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = errors.New("empty AI response")
		}
		chatResp, chatErr := chatCompletionManagedWithTimeout(cfg.Provider, client, &karmaModels.AIChatHistory{
			Messages: []karmaModels.AIMessage{{
				UniqueId:  fmt.Sprintf("internal-generation-%d", attempt+1),
				Role:      karmaModels.User,
				Message:   prompt,
				Timestamp: time.Now().UTC(),
			}},
		}, internalGenerationTimeout)
		if chatErr == nil && strings.TrimSpace(chatResp.AIResponse) != "" {
			return &GeneratedText{Text: strings.TrimSpace(chatResp.AIResponse), InputTokens: chatResp.InputTokens, OutputTokens: chatResp.OutputTokens, ModelUsed: modelUsed}, nil
		}
		if chatErr != nil {
			lastErr = chatErr
		}
		time.Sleep(time.Duration(attempt+1) * 300 * time.Millisecond)
	}
	return nil, lastErr
}

func generateFromSinglePromptWithTimeout(provider string, client *ai.KarmaAI, prompt string, timeout time.Duration) (*karmaModels.AIChatResponse, error) {
	var lastErr error
	for attempt := 1; attempt <= aiCallMaxAttempts; attempt++ {
		waitForProviderLimit(provider)
		resp, err := generateFromSinglePromptOnce(client, prompt, timeout)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !isRetryableAICallErr(err) || attempt == aiCallMaxAttempts {
			break
		}
		sleep := retryDelay(provider, err, attempt)
		noteProviderRateLimit(provider, err, sleep)
		log.Printf("[eval] ai generate retry provider=%s attempt=%d/%d delay=%s err=%v", provider, attempt+1, aiCallMaxAttempts, sleep, err)
		time.Sleep(sleep)
	}
	return nil, lastErr
}

func generateFromSinglePromptOnce(client *ai.KarmaAI, prompt string, timeout time.Duration) (*karmaModels.AIChatResponse, error) {
	type result struct {
		resp *karmaModels.AIChatResponse
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		resp, err := client.GenerateFromSinglePrompt(prompt)
		ch <- result{resp: resp, err: err}
	}()
	select {
	case res := <-ch:
		return res.resp, res.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("ai call timeout after %s", timeout)
	}
}

func chatCompletionManagedWithTimeout(provider string, client *ai.KarmaAI, history *karmaModels.AIChatHistory, timeout time.Duration) (*karmaModels.AIChatResponse, error) {
	var lastErr error
	for attempt := 1; attempt <= aiCallMaxAttempts; attempt++ {
		waitForProviderLimit(provider)
		resp, err := chatCompletionManagedOnce(client, history, timeout)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !isRetryableAICallErr(err) || attempt == aiCallMaxAttempts {
			break
		}
		sleep := retryDelay(provider, err, attempt)
		noteProviderRateLimit(provider, err, sleep)
		log.Printf("[eval] ai chat retry provider=%s attempt=%d/%d delay=%s err=%v", provider, attempt+1, aiCallMaxAttempts, sleep, err)
		time.Sleep(sleep)
	}
	return nil, lastErr
}

func chatCompletionManagedOnce(client *ai.KarmaAI, history *karmaModels.AIChatHistory, timeout time.Duration) (*karmaModels.AIChatResponse, error) {
	type result struct {
		resp *karmaModels.AIChatResponse
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		resp, err := client.ChatCompletionManaged(history)
		ch <- result{resp: resp, err: err}
	}()
	select {
	case res := <-ch:
		return res.resp, res.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("ai call timeout after %s", timeout)
	}
}

func waitForProviderLimit(provider string) {
	key := providerRateLimitKey(provider)
	for {
		raw, ok := providerRateLimitUntil.Load(key)
		if !ok {
			return
		}
		until, ok := raw.(time.Time)
		if !ok {
			providerRateLimitUntil.Delete(key)
			return
		}
		sleep := time.Until(until)
		if sleep <= 0 {
			providerRateLimitUntil.Delete(key)
			return
		}
		log.Printf("[eval] provider cooldown wait provider=%s delay=%s", provider, sleep.Round(time.Second))
		time.Sleep(sleep)
	}
}

func noteProviderRateLimit(provider string, err error, delay time.Duration) {
	if !isRateLimitErr(err) || delay <= 0 {
		return
	}
	key := providerRateLimitKey(provider)
	until := time.Now().Add(delay)
	if raw, ok := providerRateLimitUntil.Load(key); ok {
		if existing, ok := raw.(time.Time); ok && existing.After(until) {
			return
		}
	}
	providerRateLimitUntil.Store(key, until)
}

func providerRateLimitKey(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

func isRetryableAICallErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "429") ||
		strings.Contains(msg, "too many requests") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "temporarily unavailable") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "connection refused")
}

func retryDelay(provider string, err error, attempt int) time.Duration {
	if isNvidiaNIMProvider(provider) && isRateLimitErr(err) {
		if delay, ok := providerRetryDelay(err); ok {
			return delay
		}
		return time.Minute + time.Duration(attempt*5)*time.Second
	}
	delay := time.Duration(attempt*attempt) * 750 * time.Millisecond
	if delay > 8*time.Second {
		return 8 * time.Second
	}
	return delay
}

func providerRetryDelay(err error) (time.Duration, bool) {
	var rateLimitErr *ai.RateLimitError
	if errors.As(err, &rateLimitErr) && rateLimitErr.RetryAfter > 0 {
		return rateLimitErr.RetryAfter + time.Second, true
	}
	return 0, false
}

func isRateLimitErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "429") || strings.Contains(msg, "too many requests") || strings.Contains(msg, "rate limit")
}

func isNvidiaNIMProvider(provider string) bool {
	p := strings.ToLower(strings.TrimSpace(provider))
	return strings.Contains(p, "nvidia")
}

func judgeTimeoutForProvider(provider string, reasoningEffort string) time.Duration {
	p := strings.ToLower(strings.TrimSpace(provider))
	if strings.Contains(p, "xai") || strings.TrimSpace(reasoningEffort) != "" {
		return judgeCallTimeoutSlow
	}
	return judgeCallTimeout
}

func isTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded")
}

func isNoUsableJudgeErr(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "no evaluator judge returned a usable result")
}

func judgeModelKey(judge constants.EvaluatorJudgeConfig) string {
	return judge.Name + "/" + judge.Provider + "/" + judge.Model
}

func shouldCacheJudgeUnavailable(judge constants.EvaluatorJudgeConfig, err error) bool {
	if err == nil {
		return false
	}
	if isRateLimitErr(err) {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "invalid json") || strings.Contains(msg, "metric scores") || strings.Contains(msg, "schema") {
		return false
	}
	model := strings.ToLower(judge.Model)
	if strings.Contains(model, "deepseek") && isTimeoutErr(err) {
		return true
	}
	return !isTimeoutErr(err)
}

func reasoningEffort(provider string, value string) (shared.ReasoningEffort, bool) {
	if !providerSupportsReasoningEffort(provider) {
		return "", false
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "none":
		return shared.ReasoningEffortNone, true
	case "minimal":
		return shared.ReasoningEffortMinimal, true
	case "low":
		return shared.ReasoningEffortLow, true
	case "medium":
		return shared.ReasoningEffortMedium, true
	case "high", "":
		return shared.ReasoningEffortHigh, value != ""
	case "xhigh":
		return shared.ReasoningEffortXhigh, true
	default:
		return "", false
	}
}

func providerSupportsReasoningEffort(provider string) bool {
	p := strings.ToLower(strings.TrimSpace(provider))
	return !strings.Contains(p, "xai")
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

func judgeDisagreementExamples(scores []models.ConversationScore, threshold float64, limit int) []map[string]any {
	if limit <= 0 {
		return nil
	}
	sort.Slice(scores, func(i, j int) bool {
		return derefFloat(scores[i].JudgeDisagreementDelta) > derefFloat(scores[j].JudgeDisagreementDelta)
	})
	examples := make([]map[string]any, 0, limit)
	for _, score := range scores {
		if score.JudgeDisagreementDelta == nil || *score.JudgeDisagreementDelta <= threshold || score.ComplianceBreakdown == nil {
			continue
		}
		var judges []JudgeResult
		data, _ := json.Marshal(score.ComplianceBreakdown["judge_results"])
		if err := json.Unmarshal(data, &judges); err != nil || len(judges) == 0 {
			continue
		}
		judgeSummaries := make([]map[string]any, 0, len(judges))
		for _, judge := range judges {
			judgeSummaries = append(judgeSummaries, map[string]any{
				"name":                 judge.Name,
				"score":                judge.Metrics.CompositeScore,
				"compliance_pass":      judge.Metrics.CompliancePass,
				"compliance_breakdown": judge.Metrics.ComplianceBreakdown,
				"reasoning":            truncateForPrompt(judge.Metrics.Reasoning, 320),
			})
		}
		examples = append(examples, map[string]any{
			"workflow_id":         score.WorkflowId,
			"conversation_id":     score.ConversationId,
			"prompt_version":      score.PromptVersion,
			"persona":             score.PersonaType,
			"aggregate_score":     score.CompositeScore,
			"disagreement_delta":  derefFloat(score.JudgeDisagreementDelta),
			"aggregate_breakdown": score.ComplianceBreakdown,
			"judges":              judgeSummaries,
		})
		if len(examples) >= limit {
			break
		}
	}
	return examples
}

func judgeInvalidJSONStats(scores []models.ConversationScore) (float64, int) {
	if len(scores) == 0 {
		return 0, 0
	}
	invalid := 0
	checked := 0
	for _, score := range scores {
		if score.ComplianceBreakdown == nil {
			continue
		}
		checked++
		data, _ := json.Marshal(score.ComplianceBreakdown["judge_results"])
		if strings.Contains(string(data), `"valid":false`) || strings.Contains(string(data), "invalid JSON") {
			invalid++
		}
	}
	if checked == 0 {
		return 0, 0
	}
	return float64(invalid) / float64(checked), invalid
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

func rejectionReason(adopt bool, pValue, delta, effectSize, controlCompliance, treatmentCompliance float64, issueGatePassed bool, issueGateReason string) *string {
	if adopt {
		return nil
	}
	if treatmentCompliance == 0 {
		reason := fmt.Sprintf("candidate rejected: compliance remained 0.00; prompt must fix judge compliance_breakdown defects before adoption (delta=%.2f p=%.4f d=%.2f control_compliance=%.2f)", delta, pValue, effectSize, controlCompliance)
		return &reason
	}
	if !issueGatePassed {
		reason := fmt.Sprintf("candidate rejected: targeted judge issue retest failed: %s (delta=%.2f p=%.4f d=%.2f compliance %.2f->%.2f)", issueGateReason, delta, pValue, effectSize, controlCompliance, treatmentCompliance)
		return &reason
	}
	reason := fmt.Sprintf("candidate rejected: delta=%.2f p=%.4f d=%.2f compliance %.2f->%.2f did not clear adoption gates", delta, pValue, effectSize, controlCompliance, treatmentCompliance)
	return &reason
}

func targetedIssueGate(controlStats []SimulationScore, treatmentStats []SimulationScore) (bool, string) {
	controlIssues := issueCategoryRates(controlStats)
	if len(controlIssues) == 0 {
		return true, "no control judge issues found"
	}
	treatmentIssues := issueCategoryRates(treatmentStats)
	failed := []string{}
	for category, controlRate := range controlIssues {
		if controlRate < 0.2 {
			continue
		}
		treatmentRate := treatmentIssues[category]
		if treatmentRate > 0 && treatmentRate > controlRate*0.5 {
			failed = append(failed, fmt.Sprintf("%s %.2f->%.2f", category, controlRate, treatmentRate))
		}
	}
	if len(failed) > 0 {
		sort.Strings(failed)
		return false, strings.Join(failed, "; ")
	}
	return true, "targeted judge issues improved"
}

func issueCategoryRates(stats []SimulationScore) map[string]float64 {
	counts := map[string]int{}
	total := 0
	for _, score := range stats {
		for _, judge := range score.JudgeResults {
			if !judge.Valid {
				counts["invalid_judge_output"]++
				total++
				continue
			}
			categories := issueCategoriesForJudge(judge)
			if len(categories) == 0 {
				continue
			}
			total++
			for category := range categories {
				counts[category]++
			}
		}
	}
	out := map[string]float64{}
	if total == 0 {
		return out
	}
	for category, count := range counts {
		out[category] = float64(count) / float64(total)
	}
	return out
}

func issueCategoriesForJudge(judge JudgeResult) map[string]bool {
	out := map[string]bool{}
	m := judge.Metrics
	addLowMetricIssues(out, m)
	if m.CompliancePass < 10 {
		out["compliance"] = true
	}
	blobParts := []string{m.Reasoning}
	if len(m.ComplianceBreakdown) > 0 {
		if data, err := json.Marshal(m.ComplianceBreakdown); err == nil {
			blobParts = append(blobParts, string(data))
		}
	}
	blob := strings.ToLower(strings.Join(blobParts, " "))
	keywords := map[string][]string{
		"disclosure":   {"disclosure", "ai agent", "logged", "recorded", "recording"},
		"identity":     {"identity", "verify", "verification", "account"},
		"handoff":      {"handoff", "continuity", "repeated", "restart", "context"},
		"offer":        {"offer", "terms", "discount", "payment", "settlement", "unauthorized"},
		"deadline":     {"deadline", "expiry", "expires"},
		"hardship":     {"hardship", "medical", "distress", "crisis"},
		"stop_contact": {"stop contact", "no contact", "do not contact"},
		"privacy":      {"privacy", "full account", "sensitive"},
		"false_threat": {"false threat", "arrest", "garnishment", "legal threat"},
		"json_quality": {"json", "schema", "invalid"},
	}
	for category, words := range keywords {
		for _, word := range words {
			if strings.Contains(blob, word) {
				out[category] = true
				break
			}
		}
	}
	return out
}

func addLowMetricIssues(out map[string]bool, m MetricScores) {
	if m.IdentityVerified < 7 {
		out["identity"] = true
	}
	if m.ContextContinuity < 7 {
		out["handoff"] = true
	}
	if m.OfferClarity < 7 || m.NoNegotiationDrift < 7 {
		out["offer"] = true
	}
	if m.DeadlineSpecificity < 7 {
		out["deadline"] = true
	}
	if m.ConsequenceAccuracy < 7 {
		out["false_threat"] = true
	}
	if m.NoRedundancy < 7 {
		out["handoff"] = true
	}
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

func mustCurrentWorkflow(id string, previous models.BorrowerWorkflow) models.BorrowerWorkflow {
	wf, err := collections.GetWorkflow(id)
	if err != nil {
		return previous
	}
	return *wf
}

func mustConversation(id string, previous models.AgentConversation) models.AgentConversation {
	view, err := collections.ConversationByIDOrWorkflow(id)
	if err != nil {
		return previous
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
