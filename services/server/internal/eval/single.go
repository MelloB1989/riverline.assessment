package eval

import (
	"context"
	"strings"

	"riverline_server/constants"
	"riverline_server/internal/models"
)

type SingleSimulationRequest struct {
	Persona          models.Persona                    `json:"persona"`
	Seed             int64                             `json:"seed"`
	MaxTurnsPerAgent int                               `json:"max_turns_per_agent"`
	AgentID          models.AgentID                    `json:"agent_id,omitempty"`
	Judges           []constants.EvaluatorJudgeConfig  `json:"judges,omitempty"`
	PromptOverrides  map[models.AgentID]PromptOverride `json:"-"`
}

type SingleSimulationAgentOutput struct {
	Transcript string `json:"transcript,omitempty"`
	Handoff    string `json:"handoff,omitempty"`
	Turns      int    `json:"turns"`
}

type SingleSimulationResponse struct {
	WorkflowID string                                         `json:"workflow_id"`
	Agents     map[models.AgentID]SingleSimulationAgentOutput `json:"agents"`
	Scores     []SimulationScore                              `json:"scores"`
	CostUSD    float64                                        `json:"cost_usd"`
	Simulation SimulatedConversation                          `json:"simulation"`
}

func RunSingleSimulation(req SingleSimulationRequest) (*SingleSimulationResponse, error) {
	slCfg := constants.DefaultSelfLearningConfig()
	if req.Persona == "" {
		req.Persona = models.PersonaCooperative
	}
	if req.MaxTurnsPerAgent <= 0 {
		req.MaxTurnsPerAgent = slCfg.DefaultMaxTurnsPerAgent
	}
	if req.AgentID == "" {
		req.AgentID = models.AgentAria
	}
	if len(req.Judges) == 0 {
		req.Judges = slCfg.Judges
	}
	persona, err := newPersonaSimulator(slCfg)
	if err != nil {
		return nil, err
	}
	before, err := loadCostBreakdown()
	if err != nil {
		return nil, err
	}
	cfg := SimConfig{
		Seed:             req.Seed,
		BatchSize:        1,
		Personas:         []models.Persona{req.Persona},
		AgentID:          req.AgentID,
		MaxTurnsPerAgent: req.MaxTurnsPerAgent,
		Judges:           req.Judges,
		PromptOverrides:  req.PromptOverrides,
	}
	seed := simulationSeed(req.Seed, req.Persona, 0)
	sim, err := runOneSimulation(context.Background(), persona, cfg, req.Persona, seed)
	if err != nil {
		return nil, err
	}
	scores, err := ScoreSimulationsForAgent([]SimulatedConversation{sim}, req.AgentID, req.Judges)
	if err != nil {
		return nil, err
	}
	after, err := loadCostBreakdown()
	if err != nil {
		return nil, err
	}
	return &SingleSimulationResponse{
		WorkflowID: sim.Workflow.Id,
		Agents:     singleSimulationAgents(sim),
		Scores:     scores,
		CostUSD:    after.TotalUSD - before.TotalUSD,
		Simulation: sim,
	}, nil
}

func singleSimulationAgents(sim SimulatedConversation) map[models.AgentID]SingleSimulationAgentOutput {
	out := map[models.AgentID]SingleSimulationAgentOutput{}
	for _, agentID := range []models.AgentID{models.AgentAria, models.AgentNova, models.AgentDelta} {
		transcript := strings.TrimSpace(sim.AgentTranscripts[agentID])
		item := SingleSimulationAgentOutput{Turns: transcriptTurnCount(transcript)}
		if agentID == models.AgentDelta {
			item.Handoff = transcript
		} else {
			item.Transcript = transcript
		}
		out[agentID] = item
	}
	return out
}

func transcriptTurnCount(transcript string) int {
	if strings.TrimSpace(transcript) == "" {
		return 0
	}
	count := 0
	for _, line := range strings.Split(transcript, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Borrower:") || strings.HasPrefix(line, "Agent:") {
			count++
		}
	}
	if count == 0 {
		return 1
	}
	return count
}
