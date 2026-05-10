package eval

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MelloB1989/karma/ai"
	karmaModels "github.com/MelloB1989/karma/models"
	"github.com/MelloB1989/karma/utils"
	"github.com/MelloB1989/karma/v2/orm"
	"log"
	"math"
	"riverline_server/constants"
	"riverline_server/internal/collections"
	"riverline_server/internal/models"
	"sort"
	"strings"
	"time"
)

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

func judgeTimeoutForProvider(provider string, reasoningEffort string) time.Duration {
	p := strings.ToLower(strings.TrimSpace(provider))
	if strings.Contains(p, "xai") || strings.TrimSpace(reasoningEffort) != "" {
		return judgeCallTimeoutSlow
	}
	return judgeCallTimeout
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

func bounded(v float64) float64 {
	return math.Max(0, math.Min(10, v))
}
