package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MelloB1989/karma/ai"
	karmaModels "github.com/MelloB1989/karma/models"
	"github.com/MelloB1989/karma/utils"
	"github.com/MelloB1989/karma/v2/orm"
	"github.com/openai/openai-go/v3/shared"
	"log"
	"riverline_server/constants"
	"riverline_server/internal/collections"
	"riverline_server/internal/models"
	"sort"
	"strings"
	"time"
)

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
	agentTruth := constants.AgentTruthForPromptGenerator(agentID)
	lines := []string{
		fmt.Sprintf("Target prompt under test: %s.", agentID),
		"Use this guidance to make treatment simulations adversarial against defects previously found by LLM judges.",
		"Keep borrower facts consistent with the seeded users and loans rows.",
		"Do not invent contradictory identity, account, hardship, or payment-capacity facts.",
		"",
		"Agent capability boundaries (Truth):",
		agentTruth,
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
	agentTruth := constants.AgentTruthForPromptGenerator(agentID)
	prompt := fmt.Sprintf(`Generate an improved production system prompt for the %s collections agent.

%s

Current prompt:
%s

Quantitative control-run evidence and judge defects:
%s

Rewrite instructions:
- Return ONLY the complete replacement system prompt. Do not output anything else.
- VERY IMPORTANT: The new prompt MUST be strictly under 1500 tokens. This is a HARD limit. Be concise. Remove fluff. Use bullet points.
- Preserve the same agent role, tools, compliance boundaries, context budgets, borrower-facing single Riverline identity, and handoff responsibilities.
- CRITICAL: The agent truth above is the authoritative specification. Every capability listed under CAN do must be preserved. Every boundary listed under CANNOT do must be enforced. Do not add capabilities not listed. Do not remove boundaries.
- Analyze the "Quantitative control-run evidence and judge defects" provided. You MUST explicitly address and fix the specific reasons the previous prompt lost points.
- Do not remove instructions that are currently working well. Only adjust the prompt to eliminate the identified defects and improve the compliance score.
- Keep the prompt operationally precise: ordered flow, stop conditions, tool-use criteria, and failure recovery instructions.
- Make the prompt robust against the exact defects and low metrics listed above to guarantee a HIGHER evaluation score.
- HARDSHIP: The agent must offer to CONNECT with a hardship program, never create or invent hardship plan terms.

	Return ONLY the complete replacement system prompt.`, agentID, agentTruth, currentPrompt, evidence)
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

func internalPromptOptimizerSystemPrompt() string {
	return `You are Riverline's internal prompt optimization and evaluator-rubric repair service. You write production-ready agent prompts and evaluator prompts from quantitative evidence. Follow the requested output format exactly. Never roleplay as a borrower-facing collections agent. Preserve compliance, tool contracts, and context-budget constraints.

CRITICAL RULES:
- Every generated prompt must respect the agent truth (capabilities and boundaries) provided in the user prompt.
- No agent may negotiate, offer, or create capabilities not listed in its CAN DO section.
- No agent may create or invent hardship plan terms. Agents only REFER to the hardship program.
- NOVA may present a hardship referral as one resolution path (flagging for program enrollment), not a custom hardship plan with specific terms.
- Settlement offers must stay within policy_max_discount_pct from the loan data.
- Handoff summaries must fit within 500 tokens. Total agent context window is 2000 tokens.`
}

func generateInternalText(prompt string, systemPrompt string, attempts int) (*GeneratedText, error) {
	if attempts <= 0 {
		attempts = 1
	}
	slCfg := constants.DefaultSelfLearningConfig()
	cfg := slCfg.PromptGenerator
	maxTokens := slCfg.PromptGeneratorMaxTokens
	if maxTokens <= 0 {
		maxTokens = 2200
	}
	modelCfg := ai.ModelConfig{BaseModel: ai.BaseModel(cfg.Model), Provider: ai.Provider(cfg.Provider)}
	options := []ai.Option{
		ai.WithMaxTokens(maxTokens),
		ai.WithSystemMessage(systemPrompt),
		ai.WithTemperature(cfg.Temperature),
	}
	reasoningAttached := false
	if maxTokens >= 4000 {
		if effort, ok := reasoningEffort(cfg.Provider, cfg.ReasoningEffort); ok {
			reasoningAttached = true
			options = append(options, ai.WithReasoningEffort(effort))
		}
	}
	if isNvidiaNIMProvider(cfg.Provider) {
		options = append(options, ai.WithRateLimit(nvidiaNIMRequestsPerMinute, ai.RateLimitBehaviorWait))
	}
	client := ai.NewKarmaAI(
		ai.BaseModel(cfg.Model),
		ai.Provider(cfg.Provider),
		options...,
	)
	noReasoningOptions := []ai.Option{
		ai.WithMaxTokens(maxTokens),
		ai.WithSystemMessage(systemPrompt),
		ai.WithTemperature(cfg.Temperature),
	}
	if isNvidiaNIMProvider(cfg.Provider) {
		noReasoningOptions = append(noReasoningOptions, ai.WithRateLimit(nvidiaNIMRequestsPerMinute, ai.RateLimitBehaviorWait))
	}
	noReasoningClient := ai.NewKarmaAI(
		ai.BaseModel(cfg.Model),
		ai.Provider(cfg.Provider),
		noReasoningOptions...,
	)
	modelUsed := string(cfg.Provider) + "/" + modelCfg.GetModelString()
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		resp, err := generateFromSinglePromptWithTimeout(cfg.Provider, client, prompt, internalGenerationTimeout)
		if usableAIText(resp) {
			return &GeneratedText{Text: strings.TrimSpace(resp.AIResponse), InputTokens: resp.InputTokens, OutputTokens: resp.OutputTokens, ModelUsed: modelUsed}, nil
		}
		if err != nil {
			lastErr = err
		} else if allReasoningNoText(resp, maxTokens) {
			lastErr = fmt.Errorf("empty AI response after output token budget was exhausted: output_tokens=%d max_tokens=%d", resp.OutputTokens, maxTokens)
			log.Printf("[eval] internal generation exhausted output budget without text provider=%s model=%s attempt=%d/%d tokens_out=%d max_tokens=%d reasoning_attached=%t", cfg.Provider, cfg.Model, attempt+1, attempts, resp.OutputTokens, maxTokens, reasoningAttached)
		} else {
			lastErr = errors.New("empty AI response")
		}
		if reasoningAttached && allReasoningNoText(resp, maxTokens) {
			resp, err = generateFromSinglePromptWithTimeout(cfg.Provider, noReasoningClient, prompt, internalGenerationTimeout)
			if usableAIText(resp) {
				return &GeneratedText{Text: strings.TrimSpace(resp.AIResponse), InputTokens: resp.InputTokens, OutputTokens: resp.OutputTokens, ModelUsed: modelUsed}, nil
			}
			if err != nil {
				lastErr = err
			}
		}
		chatResp, chatErr := chatCompletionManagedWithTimeout(cfg.Provider, client, &karmaModels.AIChatHistory{
			Messages: []karmaModels.AIMessage{{
				UniqueId:  fmt.Sprintf("internal-generation-%d", attempt+1),
				Role:      karmaModels.User,
				Message:   prompt,
				Timestamp: time.Now().UTC(),
			}},
		}, internalGenerationTimeout)
		if usableAIText(chatResp) {
			return &GeneratedText{Text: strings.TrimSpace(chatResp.AIResponse), InputTokens: chatResp.InputTokens, OutputTokens: chatResp.OutputTokens, ModelUsed: modelUsed}, nil
		}
		if chatErr != nil {
			lastErr = chatErr
		} else if allReasoningNoText(chatResp, maxTokens) {
			lastErr = fmt.Errorf("empty AI chat response after output token budget was exhausted: output_tokens=%d max_tokens=%d", chatResp.OutputTokens, maxTokens)
		}
		reducedPrompt := reducedInternalGenerationPrompt(prompt)
		if reducedPrompt != prompt {
			resp, err = generateFromSinglePromptWithTimeout(cfg.Provider, noReasoningClient, reducedPrompt, internalGenerationTimeout)
			if usableAIText(resp) {
				return &GeneratedText{Text: strings.TrimSpace(resp.AIResponse), InputTokens: resp.InputTokens, OutputTokens: resp.OutputTokens, ModelUsed: modelUsed}, nil
			}
			if err != nil {
				lastErr = err
			} else if allReasoningNoText(resp, maxTokens) {
				lastErr = fmt.Errorf("empty reduced-context AI response after output token budget was exhausted: output_tokens=%d max_tokens=%d", resp.OutputTokens, maxTokens)
			}
		}
		time.Sleep(time.Duration(attempt+1) * 300 * time.Millisecond)
	}
	return nil, lastErr
}

func usableAIText(resp *karmaModels.AIChatResponse) bool {
	return resp != nil && strings.TrimSpace(resp.AIResponse) != ""
}

func allReasoningNoText(resp *karmaModels.AIChatResponse, maxTokens int) bool {
	if resp == nil || maxTokens <= 0 || strings.TrimSpace(resp.AIResponse) != "" {
		return false
	}
	return resp.OutputTokens >= maxTokens-50
}

func reducedInternalGenerationPrompt(prompt string) string {
	marker := "Quantitative control-run evidence"
	idx := strings.Index(prompt, marker)
	if idx < 0 {
		return prompt
	}
	return strings.TrimSpace(prompt[:idx])
}

func generateFromSinglePromptWithTimeout(provider string, client *ai.KarmaAI, prompt string, timeout time.Duration) (*karmaModels.AIChatResponse, error) {
	var lastErr error
	for attempt := 1; attempt <= aiCallMaxAttempts; attempt++ {
		waitForProviderLimit(provider)
		resp, err := generateFromSinglePromptOnce(client, prompt, timeout)
		if err == nil && resp == nil {
			err = errors.New("empty AI response")
		}
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
		if err == nil && resp == nil {
			err = errors.New("empty AI response")
		}
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
	if delay > 60*time.Second {
		delay = 60 * time.Second
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
			if delay > 60*time.Second {
				return 60 * time.Second
			}
			return delay
		}
		return 60 * time.Second
	}
	delay := time.Duration(attempt*attempt) * 750 * time.Millisecond
	if delay > 6*time.Second {
		return 6 * time.Second
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

func isTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded")
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
