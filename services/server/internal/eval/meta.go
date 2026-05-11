package eval

import (
	"encoding/json"
	"fmt"
	"log"
	"github.com/MelloB1989/karma/utils"
	"github.com/MelloB1989/karma/v2/orm"
	"math"
	"riverline_server/constants"
	"riverline_server/internal/collections"
	"riverline_server/internal/models"
	"sort"
	"strings"
	"time"
)

func RunImprovementCycle(agentID models.AgentID, cfg SimConfig) (*models.PromptExperiment, error) {
	exp, _, _, err := runImprovementCycleDetailed(agentID, cfg)
	return exp, err
}

type metaEvaluationOptions struct {
	CanaryLimit    int
	BenchmarkLimit int
	MaxFlags       int
	MaxJudgeCalls  int
	MaxDuration    time.Duration
}

func RunMetaEvaluation(agentID models.AgentID) ([]models.MetaFlag, error) {
	return runMetaEvaluation(agentID, metaEvaluationOptions{
		CanaryLimit:    2,
		BenchmarkLimit: 2,
		MaxFlags:       3,
		MaxJudgeCalls:  24,
		MaxDuration:    5 * time.Minute,
	})
}

func runMetaEvaluation(agentID models.AgentID, opts metaEvaluationOptions) ([]models.MetaFlag, error) {
	metaStart := time.Now()
	if err := collections.EnsureDefaults(); err != nil {
		return nil, err
	}
	if opts.MaxJudgeCalls <= 0 {
		opts.MaxJudgeCalls = 24
	}
	if opts.MaxDuration <= 0 {
		opts.MaxDuration = 5 * time.Minute
	}
	if opts.CanaryLimit <= 0 {
		opts.CanaryLimit = 2
	}
	if opts.BenchmarkLimit <= 0 {
		opts.BenchmarkLimit = 2
	}
	if opts.MaxFlags <= 0 {
		opts.MaxFlags = 3
	}
	slCfg := constants.DefaultSelfLearningConfig()
	scores, err := latestVersionScores(agentID)
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
	// Time guard: if meta-eval analysis took too long, skip canary+resolution
	if time.Since(metaStart) >= opts.MaxDuration {
		log.Printf("[eval] meta evaluation time guard hit agent=%s duration=%s flags=%d skipping_canaries_and_resolution", agentID, time.Since(metaStart), len(flags))
		return flags, nil
	}
	// Run canaries with limit and error recovery
	canaries, err := runCanarySetForAgent(agentID, opts.CanaryLimit)
	if err != nil {
		log.Printf("[eval] meta evaluation canary set failed agent=%s err=%v (non-fatal, continuing)", agentID, err)
	} else {
		for _, canary := range canaries {
			if canary.CorrectlyFlagged != nil && !*canary.CorrectlyFlagged {
				flags = append(flags, newMetaFlag(models.FlagTypeComplianceBlindspot, agentID, map[string]any{
					"canary_id": canary.CanaryId, "evaluator_version": canary.EvaluatorVersion,
				}, "Revise compliance checks so known canary violations are caught."))
			}
		}
	}
	if len(flags) == 0 {
		log.Printf("[eval] meta evaluation done agent=%s flags=0 duration=%s", agentID, time.Since(metaStart))
		return []models.MetaFlag{}, nil
	}
	if opts.MaxFlags > 0 && len(flags) > opts.MaxFlags {
		flags = flags[:opts.MaxFlags]
	}
	flagOrm := orm.Load(&models.MetaFlag{})
	defer flagOrm.Close()
	for i := range flags {
		// Time guard per flag
		if time.Since(metaStart) >= opts.MaxDuration {
			log.Printf("[eval] meta evaluation time guard hit during flag resolution agent=%s flag=%d/%d duration=%s", agentID, i+1, len(flags), time.Since(metaStart))
			break
		}
		if err := flagOrm.Insert(&flags[i]); err != nil {
			log.Printf("[eval] meta evaluation flag insert failed agent=%s flag=%s err=%v (non-fatal)", agentID, flags[i].Id, err)
			continue
		}
		if err := resolveMetaFlag(&flags[i]); err != nil {
			log.Printf("[eval] meta evaluation flag resolution failed agent=%s flag=%s err=%v (non-fatal, continuing)", agentID, flags[i].Id, err)
			// Mark flag as resolved-with-error so it doesn't block future cycles
			resolved := true
			errResolution := fmt.Sprintf("Resolution failed: %v", err)
			flags[i].Resolved = &resolved
			flags[i].Resolution = &errResolution
			now := time.Now().UTC()
			flags[i].ResolvedAt = &now
			_ = flagOrm.Update(&flags[i], flags[i].Id)
			continue
		}
	}
	log.Printf("[eval] meta evaluation done agent=%s flags=%d duration=%s", agentID, len(flags), time.Since(metaStart))
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

func RunProductionLearningTick() error {
	slCfg := constants.DefaultSelfLearningConfig()
	allAgents := []models.AgentID{models.AgentAria, models.AgentNova, models.AgentDelta}

	// 1. Meta Evaluation Check
	for _, agentID := range allAgents {
		scores, err := latestVersionScores(agentID)
		if err != nil {
			log.Printf("[eval] production tick failed to load scores for %s: %v", agentID, err)
			continue
		}
		if len(scores) >= slCfg.MetaEvaluationMinSample && len(scores)%slCfg.MetaEvaluationMinSample == 0 {
			if _, err := RunMetaEvaluation(agentID); err != nil {
				log.Printf("[eval] production tick meta eval failed for %s: %v", agentID, err)
			}
		}
	}

	// 2. Prompt Improvement Check
	needsImprovement := false
	for _, agentID := range allAgents {
		scores, err := latestVersionScores(agentID)
		if err == nil && recentScoresNeedPromptImprovement(scores, 8) {
			needsImprovement = true
			break
		}
	}

	if !needsImprovement || recentExperimentExists(time.Hour) {
		return nil
	}

	// Run cycle for all 3 agents (using ARIA as primary for scoring)
	primaryAgent := models.AgentAria
	cfg := SimConfig{
		Seed:                   time.Now().Unix(),
		BatchSize:              1,
		Personas:               defaultPersonas(),
		AgentID:                primaryAgent,
		MaxTurnsPerAgent:       slCfg.DefaultMaxTurnsPerAgent,
		Judges:                 slCfg.Judges,
		MaxPromptIterations:    1,
		MetaEvalEveryJudgeRuns: slCfg.MetaEvalEveryJudgeRuns,
	}
	_, err := RunImprovementCycle(primaryAgent, cfg)
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

func recentExperimentExists(window time.Duration) bool {
	o := orm.Load(&models.PromptExperiment{})
	defer o.Close()
	var rows []models.PromptExperiment
	if err := o.GetAll().Scan(&rows); err != nil {
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

func resolveMetaFlag(flag *models.MetaFlag) error {
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

	// Deactivate existing versions
	var existing []models.EvaluatorVersion
	if err := evaluatorOrm.GetByFieldEquals("AgentId", agentID).Scan(&existing); err != nil {
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

	// Activate new version
	active := true
	changeReason := fmt.Sprintf("Generated from meta-evaluation flag %s", flag.Id)
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

	flag.Evidence["revision_benchmark"] = map[string]any{"status": "adopted_without_benchmark"}
	resolved := true
	resolution := fmt.Sprintf("Created and activated evaluator version %d directly.", nextVersion)
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
	return nil
}

func generateEvaluatorRevision(agentID models.AgentID, current models.EvaluatorVersion, flag models.MetaFlag) (string, int, int, string, error) {
	evidence, _ := json.Marshal(flag.Evidence)
	agentTruth := constants.AgentTruthForPromptGenerator(agentID)
	prompt := fmt.Sprintf(`Generate a revised evaluator judge prompt for the %s collections agent.

Agent Policy/Truth Constraints (Judges MUST enforce these boundaries and capabilities):
%s

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
- Ensure the judge strictly penalizes the agent if it violates ANY "CANNOT do" constraint from the Agent Truth.
- Ensure the judge respects the agent's capabilities listed in the "CAN do" section of the Agent Truth.
- Improve stable rerun behavior for identical transcripts.

	Return only the revised judge prompt text.`, agentID, agentTruth, current.JudgePrompt, flag.FlagType, string(evidence), derefString(flag.ProposedAction))
	resp, err := generateInternalText(prompt, internalPromptOptimizerSystemPrompt(), 8)
	if err != nil {
		return "", 0, 0, "", fmt.Errorf("generate evaluator revision for %s: %w", agentID, err)
	}
	return strings.TrimSpace(resp.Text), resp.InputTokens, resp.OutputTokens, resp.ModelUsed, nil
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
