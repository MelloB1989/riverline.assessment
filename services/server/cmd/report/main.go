package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"riverline_server/internal/eval"
	"riverline_server/internal/models"

	"github.com/MelloB1989/karma/v2/orm"
)

func main() {
	output := flag.String("output", "./results", "output directory")
	flag.Parse()
	if err := os.MkdirAll(*output, 0o755); err != nil {
		log.Fatal(err)
	}
	for _, agentID := range []models.AgentID{models.AgentAria, models.AgentNova, models.AgentDelta} {
		if err := writeConversationScores(*output, agentID); err != nil {
			log.Fatal(err)
		}
		if err := writeExperiments(*output, agentID); err != nil {
			log.Fatal(err)
		}
	}
	if err := writeMetaFlags(*output); err != nil {
		log.Fatal(err)
	}
	if err := writeCanaries(*output); err != nil {
		log.Fatal(err)
	}
	if err := writeCosts(*output); err != nil {
		log.Fatal(err)
	}
	if err := writePrompts(*output); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("report generated in %s\n", *output)
}

func writeConversationScores(output string, agentID models.AgentID) error {
	o := orm.Load(&models.ConversationScore{})
	defer o.Close()
	var rows []models.ConversationScore
	if err := o.GetByFieldEquals("AgentId", agentID).Scan(&rows); err != nil {
		return err
	}
	w, closeFn, err := csvWriter(filepath.Join(output, "conversations_"+string(agentID)+".csv"))
	if err != nil {
		return err
	}
	defer closeFn()
	_ = w.Write([]string{"id", "seed", "persona_type", "prompt_version", "evaluator_version", "composite_score", "score_identity_verified", "score_info_completeness", "score_no_redundancy", "score_tone_appropriateness", "score_compliance_pass", "compliance_passed", "compliance_breakdown", "is_simulated", "created_at"})
	for _, row := range rows {
		_ = w.Write([]string{row.Id, derefString(row.Seed), derefPersona(row.PersonaType), fmt.Sprint(row.PromptVersion), fmt.Sprint(row.EvaluatorVersion), fmt.Sprintf("%.2f", row.CompositeScore), fmtFloat(row.ScoreIdentityVerified), fmtFloat(row.ScoreInfoCompleteness), fmtFloat(row.ScoreNoRedundancy), fmtFloat(row.ScoreToneAppropriateness), fmtFloat(row.ScoreCompliancePass), fmtBool(row.CompliancePassed), eval.MarshalJSON(row.ComplianceBreakdown), fmtBool(row.IsSimulated), row.CreatedAt.Format("2006-01-02T15:04:05Z07:00")})
	}
	return w.Error()
}

func writeExperiments(output string, agentID models.AgentID) error {
	o := orm.Load(&models.PromptExperiment{})
	defer o.Close()
	var rows []models.PromptExperiment
	if err := o.GetByFieldEquals("AgentId", agentID).Scan(&rows); err != nil {
		return err
	}
	w, closeFn, err := csvWriter(filepath.Join(output, "experiments_"+string(agentID)+".csv"))
	if err != nil {
		return err
	}
	defer closeFn()
	_ = w.Write([]string{"id", "control_version", "candidate_version", "control_n", "control_mean", "control_stddev", "control_median", "control_compliance_rate", "control_scores", "treatment_n", "treatment_mean", "treatment_stddev", "treatment_median", "treatment_compliance_rate", "treatment_scores", "mean_delta", "p_value", "cohens_d", "is_significant", "adopted", "rejection_reason", "experiment_cost_usd", "created_at"})
	for _, row := range rows {
		_ = w.Write([]string{row.Id, fmt.Sprint(row.ControlVersion), fmt.Sprint(row.CandidateVersion), fmt.Sprint(row.ControlN), fmt.Sprintf("%.2f", row.ControlMean), fmt.Sprintf("%.2f", row.ControlStddev), fmt.Sprintf("%.2f", row.ControlMedian), fmt.Sprintf("%.2f", row.ControlComplianceRate), eval.MarshalJSON(row.ControlScores), fmt.Sprint(row.TreatmentN), fmt.Sprintf("%.2f", row.TreatmentMean), fmt.Sprintf("%.2f", row.TreatmentStddev), fmt.Sprintf("%.2f", row.TreatmentMedian), fmt.Sprintf("%.2f", row.TreatmentComplianceRate), eval.MarshalJSON(row.TreatmentScores), fmt.Sprintf("%.2f", row.MeanDelta), fmt.Sprintf("%.4f", row.PValue), fmtFloat(row.CohensD), fmtBool(row.IsSignificant), fmt.Sprint(row.Adopted), derefString(row.RejectionReason), fmtFloat(row.ExperimentCostUsd), row.CreatedAt.Format("2006-01-02T15:04:05Z07:00")})
	}
	return w.Error()
}

func writeMetaFlags(output string) error {
	o := orm.Load(&models.MetaFlag{})
	defer o.Close()
	var rows []models.MetaFlag
	if err := o.GetAll().Scan(&rows); err != nil {
		return err
	}
	w, closeFn, err := csvWriter(filepath.Join(output, "meta_flags.csv"))
	if err != nil {
		return err
	}
	defer closeFn()
	_ = w.Write([]string{"id", "flag_type", "agent_id", "evidence", "proposed_action", "resolved", "created_at"})
	for _, row := range rows {
		_ = w.Write([]string{row.Id, string(row.FlagType), derefAgent(row.AgentId), eval.MarshalJSON(row.Evidence), derefString(row.ProposedAction), fmtBool(row.Resolved), row.CreatedAt.Format("2006-01-02T15:04:05Z07:00")})
	}
	return w.Error()
}

func writeCanaries(output string) error {
	o := orm.Load(&models.CanaryResult{})
	defer o.Close()
	var rows []models.CanaryResult
	if err := o.GetAll().Scan(&rows); err != nil {
		return err
	}
	w, closeFn, err := csvWriter(filepath.Join(output, "canary_results.csv"))
	if err != nil {
		return err
	}
	defer closeFn()
	_ = w.Write([]string{"id", "canary_id", "evaluator_version", "checker_result", "correctly_flagged", "created_at"})
	for _, row := range rows {
		_ = w.Write([]string{row.Id, row.CanaryId, fmt.Sprint(row.EvaluatorVersion), fmtBool(row.CheckerResult), fmtBool(row.CorrectlyFlagged), row.CreatedAt.Format("2006-01-02T15:04:05Z07:00")})
	}
	return w.Error()
}

func writeCosts(output string) error {
	o := orm.Load(&models.LlmCostLog{})
	defer o.Close()
	var rows []models.LlmCostLog
	if err := o.GetAll().Scan(&rows); err != nil {
		return err
	}
	w, closeFn, err := csvWriter(filepath.Join(output, "cost_breakdown.csv"))
	if err != nil {
		return err
	}
	defer closeFn()
	_ = w.Write([]string{"id", "call_type", "agent_id", "model_used", "total_tokens", "cost_usd", "created_at"})
	for _, row := range rows {
		_ = w.Write([]string{row.Id, row.CallType, derefAgent(row.AgentId), row.ModelUsed, fmt.Sprint(row.TotalTokens), fmt.Sprintf("%.6f", row.CostUsd), row.CreatedAt.Format("2006-01-02T15:04:05Z07:00")})
	}
	return w.Error()
}

func writePrompts(output string) error {
	o := orm.Load(&models.PromptVersion{})
	defer o.Close()
	var rows []models.PromptVersion
	if err := o.GetAll().Scan(&rows); err != nil {
		return err
	}
	w, closeFn, err := csvWriter(filepath.Join(output, "prompt_versions.csv"))
	if err != nil {
		return err
	}
	defer closeFn()
	_ = w.Write([]string{"id", "agent_id", "version_number", "is_active", "adoption_reason", "rejection_reason", "created_at"})
	for _, row := range rows {
		_ = w.Write([]string{row.Id, string(row.AgentId), fmt.Sprint(row.VersionNumber), fmt.Sprint(row.IsActive), derefString(row.AdoptionReason), derefString(row.RejectionReason), row.CreatedAt.Format("2006-01-02T15:04:05Z07:00")})
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

func fmtFloat(v *float64) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%.2f", *v)
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
