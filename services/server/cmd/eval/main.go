package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"riverline_server/constants"
	"riverline_server/internal/collections"
	rivereval "riverline_server/internal/eval"
	"riverline_server/internal/models"

	"github.com/MelloB1989/karma/v2/orm"
)

func main() {
	seed := flag.Int64("seed", 42, "simulation seed")
	batchSize := flag.Int("batch-size", 1, "batch size per persona")
	agent := flag.String("agent", "all", "aria, nova, delta, or all")
	personaList := flag.String("personas", "all", "comma-separated personas or all")
	maxTurns := flag.Int("max-turns", 6, "maximum simulated turns per agent stage")
	maxPromptIterations := flag.Int("max-prompt-iterations", 3, "maximum prompt generate/evaluate iterations per agent before keeping the old prompt")
	metaEvalEveryJudgeRuns := flag.Int("meta-eval-every-judge-runs", 6, "run meta-evaluation after this many LLM judge calls during the learning loop")
	maxCost := flag.Float64("max-cost", 15, "maximum incremental LLM spend in USD for this run")
	output := flag.String("output", "./eval-artifacts", "output directory for reproducible raw JSON artifacts")
	resetDB := flag.Bool("reset-db", false, "truncate Riverline application tables before seeding defaults")
	flag.Parse()

	if *resetDB {
		if err := collections.ResetApplicationData(); err != nil {
			log.Fatal(err)
		}
	} else if err := collections.EnsureDefaults(); err != nil {
		log.Fatal(err)
	}

	agents := []models.AgentID{models.AgentAria, models.AgentNova, models.AgentDelta}
	if *agent != "all" {
		agents = []models.AgentID{models.AgentID(strings.ToLower(*agent))}
	}
	personas := parsePersonas(*personaList)

	cfg := rivereval.FullCycleConfig{
		Seed:                   *seed,
		BatchSize:              *batchSize,
		Agents:                 agents,
		Personas:               personas,
		MaxCostUSD:             *maxCost,
		MaxTurnsPerAgent:       *maxTurns,
		MaxPromptIterations:    *maxPromptIterations,
		MetaEvalEveryJudgeRuns: *metaEvalEveryJudgeRuns,
	}

	printHeader("RIVERLINE SELF-LEARNING EVALUATION CYCLE")
	printConfig(cfg)

	report, err := rivereval.RunFullCycle(cfg)
	if err != nil {
		log.Fatal(err)
	}

	printReport(report)

	if err := writeArtifacts(*output, report); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nArtifacts written to: %s\n", *output)
}

func parsePersonas(value string) []models.Persona {
	all := []models.Persona{models.PersonaCooperative, models.PersonaCombative, models.PersonaEvasive, models.PersonaDistressed, models.PersonaConfused}
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" || value == "all" {
		return all
	}
	out := make([]models.Persona, 0)
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(strings.ToLower(item))
		switch models.Persona(item) {
		case models.PersonaCooperative, models.PersonaCombative, models.PersonaEvasive, models.PersonaDistressed, models.PersonaConfused:
			out = append(out, models.Persona(item))
		default:
			log.Fatalf("invalid persona %q", item)
		}
	}
	if len(out) == 0 {
		log.Fatal("at least one persona is required")
	}
	return out
}

func printHeader(title string) {
	line := strings.Repeat("=", 72)
	fmt.Printf("\n%s\n  %s\n%s\n\n", line, title, line)
}

func printSection(title string) {
	fmt.Printf("\n--- %s %s\n", title, strings.Repeat("-", max(0, 67-len(title))))
}

func printConfig(cfg rivereval.FullCycleConfig) {
	fmt.Printf("  Seed:        %d\n", cfg.Seed)
	fmt.Printf("  Batch Size:  %d per persona\n", cfg.BatchSize)
	fmt.Printf("  Max Cost:    $%.2f incremental\n", cfg.MaxCostUSD)
	fmt.Printf("  Agents:      %v\n", cfg.Agents)
	fmt.Printf("  Personas:    %v\n", cfg.Personas)
	iterations := cfg.MaxPromptIterations
	if iterations <= 0 {
		iterations = 3
	}
	totalSims := cfg.BatchSize * len(cfg.Personas) * (1 + iterations) * len(cfg.Agents)
	fmt.Printf("  Iterations:  %d prompt attempts per agent\n", iterations)
	fmt.Printf("  Meta Eval:   every %d judge calls\n", cfg.MetaEvalEveryJudgeRuns)
	fmt.Printf("  Total Sims:  up to ~%d (control + iterative treatments)\n", totalSims)
}

func printReport(report *rivereval.FullCycleReport) {
	for _, agentReport := range report.AgentReports {
		printAgentReport(agentReport)
	}

	printCostBreakdown(report.CostBreakdown)
	printPromptEvolution(report.PromptHistory)
	printFinalSummary(report)
}

func printAgentReport(ar rivereval.AgentCycleReport) {
	printHeader(fmt.Sprintf("AGENT: %s", strings.ToUpper(string(ar.AgentID))))

	printSection("EXPERIMENT RESULTS")
	exp := ar.Experiment
	stats := exp.StatSummary

	fmt.Printf("  %-30s  %-12s  %-12s  %-12s\n", "", "CONTROL", "TREATMENT", "DELTA")
	fmt.Printf("  %-30s  %-12s  %-12s  %-12s\n", "", strings.Repeat("-", 10), strings.Repeat("-", 10), strings.Repeat("-", 10))
	fmt.Printf("  %-30s  %-12.2f  %-12.2f  %+.2f\n", "Mean Composite Score", stats.ControlMean, stats.TreatmentMean, stats.MeanDelta)
	fmt.Printf("  %-30s  %-12.2f  %-12.2f\n", "Std Deviation", stats.ControlStddev, stats.TreatmentStddev)
	fmt.Printf("  %-30s  %-12.2f  %-12.2f\n", "Median", stats.ControlMedian, stats.TreatmentMedian)
	fmt.Printf("  %-30s  %-12.1f%%  %-12.1f%%\n", "Compliance Rate", stats.ControlComplianceRate*100, stats.TreatmentComplianceRate*100)
	fmt.Printf("  %-30s  %-12d  %-12d\n", "Sample Size (N)", exp.Experiment.ControlN, exp.Experiment.TreatmentN)
	fmt.Printf("  %-30s  v%-11d  v%-11d\n", "Prompt Version", exp.Experiment.ControlVersion, exp.Experiment.CandidateVersion)

	printSection("STATISTICAL ANALYSIS")
	fmt.Printf("  p-value:             %.6f  (threshold: < 0.05)\n", stats.PValue)
	fmt.Printf("  Cohen's d:           %.4f   (threshold: >= 0.35)\n", stats.CohensD)
	fmt.Printf("  Mean Delta:          %+.2f   (threshold: >= 5.0)\n", stats.MeanDelta)
	fmt.Printf("  Significant:         %s\n", boolToYesNo(stats.IsSignificant))

	printSection("ADOPTION DECISION")
	if exp.Adopted {
		fmt.Printf("  Result:   ADOPTED\n")
	} else {
		fmt.Printf("  Result:   REJECTED\n")
	}
	fmt.Printf("  Details:  %s\n", exp.Decision)

	printSection("PER-PERSONA BREAKDOWN (CONTROL)")
	printPersonaTable(exp.ControlByPersona)

	printSection("PER-PERSONA BREAKDOWN (TREATMENT)")
	printPersonaTable(exp.TreatmentByPersona)

	printSection("PER-JUDGE BREAKDOWN (CONTROL)")
	printJudgeTable(exp.ControlByJudge)

	printSection("PER-JUDGE BREAKDOWN (TREATMENT)")
	printJudgeTable(exp.TreatmentByJudge)

	printSection("RAW SCORES")
	fmt.Printf("  Control:    %s\n", formatScoreArray(exp.Experiment.ControlScores))
	fmt.Printf("  Treatment:  %s\n", formatScoreArray(exp.Experiment.TreatmentScores))

	printSection("META-EVALUATION")
	if ar.MetaEval.FlagCount == 0 {
		fmt.Printf("  No flags detected. Evaluation methodology is healthy.\n")
	} else {
		fmt.Printf("  Flags detected: %d  |  Resolved: %d\n", ar.MetaEval.FlagCount, ar.MetaEval.ResolvedCount)
		for i, flag := range ar.MetaEval.Flags {
			resolved := "pending"
			if derefBool(flag.Resolved) {
				resolved = "resolved"
			}
			fmt.Printf("  [%d] Type: %-30s Status: %s\n", i+1, flag.FlagType, resolved)
			if flag.ProposedAction != nil {
				fmt.Printf("      Action: %s\n", *flag.ProposedAction)
			}
			if flag.EvaluatorVersionBefore != nil && flag.EvaluatorVersionAfter != nil {
				fmt.Printf("      Evaluator: v%d -> v%d\n", *flag.EvaluatorVersionBefore, *flag.EvaluatorVersionAfter)
			}
		}
	}

	printSection("CANARY TESTS")
	if ar.Canaries.TotalCanaries == 0 {
		fmt.Printf("  No canaries configured for %s.\n", ar.AgentID)
	} else {
		fmt.Printf("  Total: %d  |  Passed: %d  |  Failed: %d\n", ar.Canaries.TotalCanaries, ar.Canaries.Passed, ar.Canaries.Failed)
		for i, result := range ar.Canaries.Results {
			status := "PASS"
			if !derefBool(result.CorrectlyFlagged) {
				status = "FAIL"
			}
			fmt.Printf("  [%d] Canary %s: %s (evaluator v%d)\n", i+1, result.CanaryId[:8], status, result.EvaluatorVersion)
		}
	}

	fmt.Printf("\n  Agent cycle duration: %.1fs\n", ar.DurationSec)
}

func printPersonaTable(byPersona map[models.Persona]rivereval.PersonaStats) {
	fmt.Printf("  %-15s  %-6s  %-10s  %-10s  %-12s\n", "Persona", "N", "Mean", "Stddev", "Compliance")
	fmt.Printf("  %-15s  %-6s  %-10s  %-10s  %-12s\n", strings.Repeat("-", 15), "----", "--------", "--------", "----------")
	for _, persona := range []models.Persona{models.PersonaCooperative, models.PersonaCombative, models.PersonaEvasive, models.PersonaDistressed, models.PersonaConfused} {
		stats, ok := byPersona[persona]
		if !ok {
			continue
		}
		fmt.Printf("  %-15s  %-6d  %-10.2f  %-10.2f  %-12.1f%%\n", persona, stats.N, stats.Mean, stats.Stddev, stats.ComplianceRate*100)
	}
}

func printJudgeTable(byJudge map[string]rivereval.JudgeStats) {
	fmt.Printf("  %-15s  %-6s  %-10s  %-10s  %-12s  %-10s\n", "Judge", "N", "Mean", "Stddev", "Compliance", "Cost")
	fmt.Printf("  %-15s  %-6s  %-10s  %-10s  %-12s  %-10s\n", strings.Repeat("-", 15), "----", "--------", "--------", "----------", "--------")
	for judge, stats := range byJudge {
		fmt.Printf("  %-15s  %-6d  %-10.2f  %-10.2f  %-12.1f%%  $%-9.6f\n", judge, stats.N, stats.Mean, stats.Stddev, stats.ComplianceRate*100, stats.MeanCostUSD)
	}
}

func printCostBreakdown(cost rivereval.CostBreakdown) {
	printHeader("COST BREAKDOWN")
	fmt.Printf("  Total LLM Spend:          $%.4f\n", cost.TotalUSD)
	fmt.Printf("  Total Prompt Tokens:      %d\n", cost.TotalPromptTokens)
	fmt.Printf("  Total Completion Tokens:  %d\n", cost.TotalCompletionTokens)

	if len(cost.ByUsageType) > 0 {
		fmt.Printf("\n  By Usage Type:\n")
		for usageType, amount := range cost.ByUsageType {
			fmt.Printf("    %-30s  $%.4f\n", usageType, amount)
		}
	}
	if len(cost.ByModel) > 0 {
		fmt.Printf("\n  By Model:\n")
		for model, amount := range cost.ByModel {
			fmt.Printf("    %-40s  $%.4f\n", model, amount)
		}
	}
}

func printPromptEvolution(versions []models.PromptVersion) {
	printHeader("PROMPT VERSION HISTORY")
	if len(versions) == 0 {
		fmt.Printf("  No prompt versions found.\n")
		return
	}

	byAgent := map[models.AgentID][]models.PromptVersion{}
	for _, v := range versions {
		byAgent[v.AgentId] = append(byAgent[v.AgentId], v)
	}

	for _, agentID := range []models.AgentID{models.AgentAria, models.AgentNova, models.AgentDelta} {
		agentVersions, ok := byAgent[agentID]
		if !ok {
			continue
		}
		fmt.Printf("  %s:\n", strings.ToUpper(string(agentID)))
		for _, v := range agentVersions {
			active := " "
			if v.IsActive {
				active = "*"
			}
			reason := ""
			if v.AdoptionReason != nil {
				reason = truncate(*v.AdoptionReason, 60)
			} else if v.RejectionReason != nil {
				reason = truncate(*v.RejectionReason, 60)
			}
			fmt.Printf("    %s v%-3d  %s  %s\n", active, v.VersionNumber, v.CreatedAt.Format(time.RFC3339)[:19], reason)
		}
	}
}

func printFinalSummary(report *rivereval.FullCycleReport) {
	printHeader("FINAL SUMMARY")

	totalScored := 0
	adopted := 0
	rejected := 0
	totalFlags := 0
	totalCanariesPassed := 0
	totalCanariesFailed := 0

	for _, ar := range report.AgentReports {
		totalScored += ar.Experiment.Experiment.ControlN + ar.Experiment.Experiment.TreatmentN
		if ar.Experiment.Adopted {
			adopted++
		} else {
			rejected++
		}
		totalFlags += ar.MetaEval.FlagCount
		totalCanariesPassed += ar.Canaries.Passed
		totalCanariesFailed += ar.Canaries.Failed
	}

	fmt.Printf("  Agents Evaluated:     %d\n", len(report.AgentReports))
	fmt.Printf("  Total Scored:         %d conversations\n", totalScored)
	fmt.Printf("  Prompts Adopted:      %d\n", adopted)
	fmt.Printf("  Prompts Rejected:     %d\n", rejected)
	fmt.Printf("  Meta-Eval Flags:      %d\n", totalFlags)
	fmt.Printf("  Canaries Passed:      %d / %d\n", totalCanariesPassed, totalCanariesPassed+totalCanariesFailed)
	fmt.Printf("  Total LLM Cost:       $%.4f\n", report.CostBreakdown.TotalUSD)
	fmt.Printf("  Total Duration:       %.1fs\n", report.DurationSec)
	fmt.Printf("  Seed:                 %d\n", report.Config.Seed)

	fmt.Printf("\n  Per-Agent Summary:\n")
	fmt.Printf("  %-10s  %-12s  %-12s  %-10s  %-10s  %-8s\n", "Agent", "Ctrl Mean", "Treat Mean", "Delta", "p-value", "Decision")
	fmt.Printf("  %-10s  %-12s  %-12s  %-10s  %-10s  %-8s\n", "--------", "----------", "----------", "--------", "--------", "--------")
	for _, ar := range report.AgentReports {
		s := ar.Experiment.StatSummary
		decision := "REJECT"
		if ar.Experiment.Adopted {
			decision = "ADOPT"
		}
		fmt.Printf("  %-10s  %-12.2f  %-12.2f  %+-10.2f  %-10.4f  %-8s\n",
			strings.ToUpper(string(ar.AgentID)), s.ControlMean, s.TreatmentMean, s.MeanDelta, s.PValue, decision)
	}

	fmt.Printf("\n%s\n", strings.Repeat("=", 72))
}

func formatScoreArray(scores []float64) string {
	parts := make([]string, len(scores))
	for i, s := range scores {
		parts[i] = fmt.Sprintf("%.2f", s)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func boolToYesNo(v bool) string {
	if v {
		return "YES"
	}
	return "NO"
}

func truncate(s string, maxLen int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func derefBool(v *bool) bool {
	return v != nil && *v
}

func writeArtifacts(output string, report *rivereval.FullCycleReport) error {
	if err := os.MkdirAll(output, 0o755); err != nil {
		return err
	}

	runConfig := map[string]any{
		"seed":         report.Config.Seed,
		"batch_size":   report.Config.BatchSize,
		"agents":       report.Config.Agents,
		"personas":     report.Config.Personas,
		"started_at":   report.StartedAt,
		"completed_at": report.CompletedAt,
		"duration_sec": report.DurationSec,
	}
	if err := writeJSON(filepath.Join(output, "run_config.json"), runConfig); err != nil {
		return err
	}

	if err := writeJSON(filepath.Join(output, "full_report.json"), report); err != nil {
		return err
	}

	metrics, err := rivereval.LoadMetrics()
	if err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(output, "metrics.json"), metrics); err != nil {
		return err
	}

	if err := writeTable[models.ConversationScore](filepath.Join(output, "conversation_scores.json"), &models.ConversationScore{}); err != nil {
		return err
	}
	if err := writeConversationScoresCSV(filepath.Join(output, "conversation_scores.csv")); err != nil {
		return err
	}
	if err := writeJudgeScoresCSV(filepath.Join(output, "judge_scores.csv")); err != nil {
		return err
	}
	if err := writeTable[models.PromptExperiment](filepath.Join(output, "prompt_experiments.json"), &models.PromptExperiment{}); err != nil {
		return err
	}
	if err := writePromptExperimentsCSV(filepath.Join(output, "prompt_experiments.csv")); err != nil {
		return err
	}
	if err := writeTable[models.MetaFlag](filepath.Join(output, "meta_flags.json"), &models.MetaFlag{}); err != nil {
		return err
	}
	if err := writeTable[models.EvaluatorVersion](filepath.Join(output, "evaluator_versions.json"), &models.EvaluatorVersion{}); err != nil {
		return err
	}
	if err := writeTable[models.CanaryResult](filepath.Join(output, "canary_results.json"), &models.CanaryResult{}); err != nil {
		return err
	}
	if err := writeTable[models.LlmCostLog](filepath.Join(output, "llm_cost_log.json"), &models.LlmCostLog{}); err != nil {
		return err
	}
	if err := writeCostCSV(filepath.Join(output, "llm_cost_log.csv")); err != nil {
		return err
	}
	if err := writeTable[models.PromptVersion](filepath.Join(output, "prompt_versions.json"), &models.PromptVersion{}); err != nil {
		return err
	}
	if err := writeLearningProof(filepath.Join(output, "learning_proof.json"), report); err != nil {
		return err
	}
	return nil
}

func writeLearningProof(path string, report *rivereval.FullCycleReport) error {
	metrics, err := rivereval.LoadMetrics()
	if err != nil {
		return err
	}
	metaFlags, err := loadRows[models.MetaFlag](&models.MetaFlag{})
	if err != nil {
		return err
	}
	canaryResults, err := loadRows[models.CanaryResult](&models.CanaryResult{})
	if err != nil {
		return err
	}
	evaluatorRows, err := loadRows[models.EvaluatorVersion](&models.EvaluatorVersion{})
	if err != nil {
		return err
	}

	agentProofs := map[models.AgentID]any{}
	for _, ar := range report.AgentReports {
		exp := ar.Experiment.Experiment
		controlPrompt := promptVersionText(report.PromptHistory, ar.AgentID, exp.ControlVersion)
		candidatePrompt := promptVersionText(report.PromptHistory, ar.AgentID, exp.CandidateVersion)
		agentProofs[ar.AgentID] = map[string]any{
			"control_version":           exp.ControlVersion,
			"candidate_version":         exp.CandidateVersion,
			"adopted":                   exp.Adopted,
			"decision":                  ar.Experiment.Decision,
			"control_mean":              exp.ControlMean,
			"treatment_mean":            exp.TreatmentMean,
			"mean_delta":                exp.MeanDelta,
			"control_compliance_rate":   exp.ControlComplianceRate,
			"treatment_compliance_rate": exp.TreatmentComplianceRate,
			"control_scores":            exp.ControlScores,
			"treatment_scores":          exp.TreatmentScores,
			"control_by_judge":          ar.Experiment.ControlByJudge,
			"treatment_by_judge":        ar.Experiment.TreatmentByJudge,
			"control_prompt_chars":      len(controlPrompt),
			"candidate_prompt_chars":    len(candidatePrompt),
			"control_prompt_excerpt":    truncate(controlPrompt, 1200),
			"candidate_prompt_excerpt":  truncate(candidatePrompt, 1200),
			"control_prompt_full":       controlPrompt,
			"candidate_prompt_full":     candidatePrompt,
			"meta_flags":                ar.MetaEval.Flags,
			"canaries_passed":           ar.Canaries.Passed,
			"canaries_failed":           ar.Canaries.Failed,
		}
	}
	proof := map[string]any{
		"repro_command":            strings.Join(os.Args, " "),
		"prompt_generator":         constants.DefaultSelfLearningConfig().PromptGenerator,
		"seed":                     report.Config.Seed,
		"batch_size":               report.Config.BatchSize,
		"personas":                 report.Config.Personas,
		"agents":                   report.Config.Agents,
		"started_at":               report.StartedAt,
		"completed_at":             report.CompletedAt,
		"duration_sec":             report.DurationSec,
		"cost_breakdown":           report.CostBreakdown,
		"metrics":                  metrics,
		"total_scores":             metrics.TotalScores,
		"total_cost_usd":           metrics.TotalCostUSD,
		"agent_prompt_evolution":   agentProofs,
		"meta_evaluator_flags":     metaFlags,
		"canary_results":           canaryResults,
		"evaluator_versions":       evaluatorRows,
		"meta_evaluator_evolution": buildEvaluatorEvolution(evaluatorRows, metaFlags),
		"prompt_versions":          report.PromptHistory,
	}
	return writeJSON(path, proof)
}

func buildEvaluatorEvolution(versions []models.EvaluatorVersion, flags []models.MetaFlag) map[models.AgentID]any {
	out := map[models.AgentID]any{}
	byAgent := map[models.AgentID][]models.EvaluatorVersion{}
	for _, version := range versions {
		byAgent[version.AgentId] = append(byAgent[version.AgentId], version)
	}
	for agentID, agentVersions := range byAgent {
		entry := map[string]any{
			"versions": agentVersions,
			"flags":    []models.MetaFlag{},
		}
		for _, flag := range flags {
			if flag.AgentId != nil && *flag.AgentId == agentID {
				entry["flags"] = append(entry["flags"].([]models.MetaFlag), flag)
			}
		}
		out[agentID] = entry
	}
	return out
}

func promptVersionText(versions []models.PromptVersion, agentID models.AgentID, version int) string {
	for _, row := range versions {
		if row.AgentId == agentID && row.VersionNumber == version {
			return row.PromptText
		}
	}
	return ""
}

func writeTable[T any](path string, model any) error {
	rows, err := loadRows[T](model)
	if err != nil {
		return err
	}
	return writeJSON(path, rows)
}

func loadRows[T any](model any) ([]T, error) {
	o := orm.Load(model)
	defer o.Close()
	var rows []T
	if err := o.GetAll().Scan(&rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func writeConversationScoresCSV(path string) error {
	o := orm.Load(&models.ConversationScore{})
	defer o.Close()
	var rows []models.ConversationScore
	if err := o.GetAll().Scan(&rows); err != nil {
		return err
	}
	w, closeFn, err := csvWriter(path)
	if err != nil {
		return err
	}
	defer closeFn()
	_ = w.Write([]string{"id", "workflow_id", "conversation_id", "seed", "persona_type", "prompt_version", "evaluator_version", "composite_score", "compliance_passed", "judge_disagreement_delta", "eval_cost_usd", "eval_model_used", "is_simulated", "created_at"})
	for _, row := range rows {
		_ = w.Write([]string{row.Id, derefString(row.WorkflowId), row.ConversationId, derefString(row.Seed), derefPersona(row.PersonaType), fmt.Sprint(row.PromptVersion), fmt.Sprint(row.EvaluatorVersion), fmt.Sprintf("%.2f", row.CompositeScore), fmtBool(row.CompliancePassed), fmtFloat(row.JudgeDisagreementDelta), fmtFloat(row.EvalCostUsd), derefString(row.EvalModelUsed), fmtBool(row.IsSimulated), row.CreatedAt.Format(time.RFC3339)})
	}
	return w.Error()
}

func writeJudgeScoresCSV(path string) error {
	o := orm.Load(&models.ConversationScore{})
	defer o.Close()
	var rows []models.ConversationScore
	if err := o.GetAll().Scan(&rows); err != nil {
		return err
	}
	w, closeFn, err := csvWriter(path)
	if err != nil {
		return err
	}
	defer closeFn()
	_ = w.Write([]string{"score_id", "workflow_id", "conversation_id", "seed", "persona_type", "prompt_version", "evaluator_version", "judge_name", "judge_model", "judge_provider", "judge_weight", "composite_score", "compliance_pass", "input_tokens", "output_tokens", "cost_usd", "reasoning", "created_at"})
	for _, row := range rows {
		for _, judge := range judgeRows(row) {
			_ = w.Write([]string{
				row.Id,
				derefString(row.WorkflowId),
				row.ConversationId,
				derefString(row.Seed),
				derefPersona(row.PersonaType),
				fmt.Sprint(row.PromptVersion),
				fmt.Sprint(row.EvaluatorVersion),
				judge.Name,
				judge.ModelUsed,
				judge.Provider,
				fmt.Sprintf("%.2f", judge.Weight),
				fmt.Sprintf("%.2f", judge.Metrics.CompositeScore),
				fmt.Sprintf("%.0f", judge.Metrics.CompliancePass),
				fmt.Sprint(judge.InputTokens),
				fmt.Sprint(judge.OutputTokens),
				fmt.Sprintf("%.6f", judge.CostUSD),
				judge.Metrics.Reasoning,
				row.CreatedAt.Format(time.RFC3339),
			})
		}
	}
	return w.Error()
}

func judgeRows(row models.ConversationScore) []rivereval.JudgeResult {
	if row.ComplianceBreakdown == nil {
		return nil
	}
	raw, ok := row.ComplianceBreakdown["judge_results"]
	if !ok {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var judges []rivereval.JudgeResult
	if err := json.Unmarshal(data, &judges); err != nil {
		return nil
	}
	return judges
}

func writePromptExperimentsCSV(path string) error {
	o := orm.Load(&models.PromptExperiment{})
	defer o.Close()
	var rows []models.PromptExperiment
	if err := o.GetAll().Scan(&rows); err != nil {
		return err
	}
	w, closeFn, err := csvWriter(path)
	if err != nil {
		return err
	}
	defer closeFn()
	_ = w.Write([]string{"id", "agent_id", "control_version", "candidate_version", "control_n", "control_mean", "control_stddev", "control_compliance_rate", "treatment_n", "treatment_mean", "treatment_stddev", "treatment_compliance_rate", "mean_delta", "p_value", "cohens_d", "is_significant", "adopted", "rejection_reason", "experiment_cost_usd", "created_at"})
	for _, row := range rows {
		_ = w.Write([]string{row.Id, string(row.AgentId), fmt.Sprint(row.ControlVersion), fmt.Sprint(row.CandidateVersion), fmt.Sprint(row.ControlN), fmt.Sprintf("%.2f", row.ControlMean), fmt.Sprintf("%.2f", row.ControlStddev), fmt.Sprintf("%.4f", row.ControlComplianceRate), fmt.Sprint(row.TreatmentN), fmt.Sprintf("%.2f", row.TreatmentMean), fmt.Sprintf("%.2f", row.TreatmentStddev), fmt.Sprintf("%.4f", row.TreatmentComplianceRate), fmt.Sprintf("%.2f", row.MeanDelta), fmt.Sprintf("%.6f", row.PValue), fmtFloat(row.CohensD), fmtBool(row.IsSignificant), fmt.Sprint(row.Adopted), derefString(row.RejectionReason), fmtFloat(row.ExperimentCostUsd), row.CreatedAt.Format(time.RFC3339)})
	}
	return w.Error()
}

func writeCostCSV(path string) error {
	o := orm.Load(&models.LlmCostLog{})
	defer o.Close()
	var rows []models.LlmCostLog
	if err := o.GetAll().Scan(&rows); err != nil {
		return err
	}
	w, closeFn, err := csvWriter(path)
	if err != nil {
		return err
	}
	defer closeFn()
	_ = w.Write([]string{"id", "call_type", "agent_id", "model_used", "prompt_tokens", "completion_tokens", "total_tokens", "cost_usd", "conversation_id", "experiment_id", "created_at"})
	for _, row := range rows {
		_ = w.Write([]string{row.Id, row.CallType, derefAgent(row.AgentId), row.ModelUsed, fmt.Sprint(row.PromptTokens), fmt.Sprint(row.CompletionTokens), fmt.Sprint(row.TotalTokens), fmt.Sprintf("%.6f", row.CostUsd), derefString(row.ConversationId), derefString(row.ExperimentId), row.CreatedAt.Format(time.RFC3339)})
	}
	return w.Error()
}

func csvWriter(path string) (*csv.Writer, func(), error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, err
	}
	w := csv.NewWriter(f)
	return w, func() { w.Flush(); _ = f.Close() }, nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func fmtFloat(v *float64) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%.6f", *v)
}

func fmtBool(v *bool) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(*v)
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func derefAgent(v *models.AgentID) string {
	if v == nil {
		return ""
	}
	return string(*v)
}

func derefPersona(v *models.Persona) string {
	if v == nil {
		return ""
	}
	return string(*v)
}
