package eval

import (
	"context"
	"errors"
	"fmt"
	"log"
	"riverline_server/constants"
	"riverline_server/internal/agents"
	"riverline_server/internal/collections"
	"riverline_server/internal/models"
	"strings"
	"time"

	"github.com/MelloB1989/karma/utils"
	"github.com/MelloB1989/karma/v2/orm"
)

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
		return conversations, scores, errors.New("no complete simulations reached ARIA and NOVA; judges were not run on partial transcripts")
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
	if wf.CurrentStage == models.AgentAria {
		log.Printf("[eval] workflow still on aria after completion; forcing nova workflow=%s", wf.Id)
		if err := collections.ForceAdvanceToNova(wf.Id); err != nil {
			return result, err
		}
		wf, err = collections.GetWorkflow(wf.Id)
		if err != nil {
			return result, err
		}
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
	} else {
		result.Metadata["partial"] = true
		result.Metadata["partial_reason"] = fmt.Sprintf("workflow did not advance to nova after aria completion: current_stage=%s", wf.CurrentStage)
		log.Printf("[eval] nova skipped workflow=%s current_stage=%s reason=force_advance_failed", wf.Id, wf.CurrentStage)
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
		if agentText != "" {
			agentMsg, err := insertMessage(conv, models.MessageRoleAgent, agentText, resp.OutputTokens)
			if err != nil {
				return conv, false, err
			}
			messages = append(messages, agentMsg)
		}
		_ = collections.LogCost("agent_response", &conv.AgentId, client.ModelUsed(), resp.InputTokens, resp.OutputTokens, &conv.Id, nil)
		log.Printf("[eval] aria turn agent workflow=%s conversation=%s turn=%d response_chars=%d tokens_in=%d tokens_out=%d handoff=%t duration=%s", wf.Id, conv.Id, turn+1, len(agentText), resp.InputTokens, resp.OutputTokens, toolResults.AriaHandoff != nil, time.Since(turnStart))
		if toolResults.AriaHandoff != nil {
			if err := collections.ApplyAriaHandoffForSimulation(wf, toolResults.AriaHandoff.Result); err != nil {
				return conv, false, err
			}
			if err := collections.CompleteARIA(wf.Id); err != nil {
				return conv, false, err
			}
			updated, err := collections.GetWorkflow(wf.Id)
			if err != nil {
				return conv, false, err
			}
			if updated.CurrentStage == models.AgentAria {
				log.Printf("[eval] aria completion left simulated workflow on aria; forcing nova workflow=%s", wf.Id)
				if err := collections.ForceAdvanceToNova(wf.Id); err != nil {
					return conv, false, err
				}
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
	for attempt := 0; attempt < 5; attempt++ {
		callStart := time.Now()
		log.Printf("[eval] persona call start persona=%s stage=%s seed=%s attempt=%d transcript_chars=%d context_chars=%d", persona, stage, seed, attempt+1, len(transcript), len(borrowerContext))
		callCtx, cancel := context.WithTimeout(ctx, time.Duration(15+5*(attempt+1))*time.Second)
		resp, err := p.client.ChatWithTokenUsage(callCtx, messages, 0.35, 4000)
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
		if strings.EqualFold(strings.TrimSpace(resp.StopReason), "max_tokens") && borrowerMessageEndsCleanly(content) {
			log.Printf("[eval] persona truncate accepted persona=%s stage=%s seed=%s attempt=%d model=%s stop_reason=%s tokens_in=%d tokens_out=%d chars=%d duration=%s text=%q", persona, stage, seed, attempt+1, resp.Model, resp.StopReason, resp.InputTokens, resp.OutputTokens, len(content), time.Since(callStart), previewText(content, 120))
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
		return msg, errors.New("message content is empty")
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
		strings.TrimSpace(sim.Transcript) != ""
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

func borrowerMessageEndsCleanly(content string) bool {
	content = strings.TrimSpace(content)
	if content == "" {
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
