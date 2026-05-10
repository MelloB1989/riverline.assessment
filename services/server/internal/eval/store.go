package eval

import (
	"context"
	"errors"
	"fmt"
	"github.com/MelloB1989/karma/v2/orm"
	"log"
	"riverline_server/internal/collections"
	"riverline_server/internal/models"
	"sort"
	"strings"
	"time"
)

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
	// Group by agent+prompt version for per-prompt tracking
	byAgentPrompt := map[string][]models.ConversationScore{}
	for _, score := range scores {
		key := fmt.Sprintf("%s:v%d", score.AgentId, score.PromptVersion)
		byAgentPrompt[key] = append(byAgentPrompt[key], score)
	}
	out := &EvalMetrics{
		TotalScores:       len(scores),
		TotalCostUSD:      totalCost,
		SystemAggregate:   aggregateScoreRows(scores),
		ByAgentPrompt:     map[string]MetricAggregate{},
		PromptExperiments: experiments,
	}
	for key, rows := range byAgentPrompt {
		out.ByAgentPrompt[key] = aggregateScoreRows(rows)
	}
	return out, nil
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
