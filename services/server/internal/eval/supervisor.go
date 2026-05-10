package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"riverline_server/constants"
	"riverline_server/internal/collections"
	"riverline_server/internal/models"
)

type SupervisorConfig struct {
	Enabled                bool             `json:"enabled"`
	AgentsRotation         []models.AgentID `json:"agents_rotation"`
	Personas               []models.Persona `json:"personas"`
	SimsPerPersonaPerCycle int              `json:"sims_per_persona_per_cycle"`
	PromptGenEveryNScores  int              `json:"prompt_gen_every_n_scores"`
	MetaEvalEveryNJudges   int              `json:"meta_eval_every_n_judges"`
	MaxIncrementalCostUSD  float64          `json:"max_incremental_cost_usd"`
	MaxRunDuration         time.Duration    `json:"max_run_duration"`
	Seed                   int64            `json:"seed"`
}

type SupervisorStatus struct {
	Running                 bool           `json:"running"`
	StartedAt               time.Time      `json:"started_at"`
	LastActivity            time.Time      `json:"last_activity"`
	Cycles                  int            `json:"cycles"`
	ScoresSinceGen          int            `json:"scores_since_gen"`
	JudgeRunsSinceMeta      int            `json:"judge_runs_since_meta"`
	SpentUSDIncremental     float64        `json:"spent_usd_incremental"`
	CurrentAgent            models.AgentID `json:"current_agent"`
	LastError               string         `json:"last_error"`
	StopReason              string         `json:"stop_reason,omitempty"`
	RejectedCandidatesInRow int            `json:"rejected_candidates_in_row"`
	ConsecutiveErrors       int            `json:"consecutive_errors"`
}

type Supervisor struct {
	mu       sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	cfg      SupervisorConfig
	baseline float64
	status   SupervisorStatus
}

var learningSupervisor = &Supervisor{}

var (
	supervisorTotalCostUSD        = currentTotalCostUSD
	supervisorIncrementalSpentUSD = IncrementalSpentUSD
	supervisorRunCycle            = func(s *Supervisor, agentID models.AgentID, cfg SupervisorConfig) error {
		return s.runCycle(agentID, cfg)
	}
)

func StartSupervisor(cfg SupervisorConfig) error {
	return learningSupervisor.Start(cfg)
}

func StopSupervisor() error {
	return learningSupervisor.Stop()
}

func CurrentSupervisorStatus() SupervisorStatus {
	return learningSupervisor.Status()
}

func (s *Supervisor) Start(cfg SupervisorConfig) error {
	cfg = normalizeSupervisorConfig(cfg)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status.Running {
		return errors.New("learning supervisor already running")
	}
	baseline, err := supervisorTotalCostUSD()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	now := time.Now().UTC()
	s.ctx = ctx
	s.cancel = cancel
	s.done = make(chan struct{})
	s.cfg = cfg
	s.baseline = baseline
	s.status = SupervisorStatus{
		Running:      true,
		StartedAt:    now,
		LastActivity: now,
	}
	go s.run()
	return nil
}

func (s *Supervisor) Stop() error {
	s.mu.Lock()
	if !s.status.Running {
		s.mu.Unlock()
		return nil
	}
	cancel := s.cancel
	done := s.done
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			return errors.New("learning supervisor did not stop within 2s")
		}
	}
	return nil
}

func (s *Supervisor) Status() SupervisorStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *Supervisor) run() {
	defer func() {
		if recovered := recover(); recovered != nil {
			s.setError(fmt.Sprintf("panic: %v", recovered))
		}
		s.mu.Lock()
		s.status.Running = false
		s.status.LastActivity = time.Now().UTC()
		s.mu.Unlock()
		close(s.done)
	}()

	agentIndex := 0
	for {
		select {
		case <-s.ctx.Done():
			s.setStopReason("stopped")
			return
		default:
		}

		cfg := s.config()
		if cfg.MaxRunDuration > 0 && time.Since(s.startedAt()) >= cfg.MaxRunDuration {
			s.setStopReason("duration")
			return
		}
		spent, err := supervisorIncrementalSpentUSD(s.baseline)
		if err != nil {
			s.setError(err.Error())
			s.sleepOrStop(30 * time.Second)
			continue
		}
		s.updateSpent(spent)
		if cfg.MaxIncrementalCostUSD > 0 && spent >= cfg.MaxIncrementalCostUSD {
			s.setStopReason("budget")
			return
		}

		agentID := cfg.AgentsRotation[agentIndex%len(cfg.AgentsRotation)]
		agentIndex++
		s.setCurrentAgent(agentID)

		// Safety valve: max 100 cycles
		status := s.Status()
		if status.Cycles >= 100 {
			s.setStopReason("max_cycles")
			return
		}

		if err := supervisorRunCycle(s, agentID, cfg); err != nil {
			s.setError(err.Error())
			s.incrementConsecutiveErrors()
			consec := s.consecutiveErrorCount()
			log.Printf("[eval] learning supervisor cycle failed agent=%s consecutive_errors=%d err=%v", agentID, consec, err)
			// Backoff: 30s normally, 2min after 5 consecutive failures
			backoff := 30 * time.Second
			if consec >= 5 {
				backoff = 2 * time.Minute
			}
			s.sleepOrStop(backoff)
			continue
		}
		s.resetConsecutiveErrors()
	}
}

func (s *Supervisor) runCycle(agentID models.AgentID, cfg SupervisorConfig) error {
	if err := collections.EnsureDefaults(); err != nil {
		return err
	}
	bottom, err := lowScoringSimulationScores(agentID, 5)
	if err != nil {
		return err
	}
	simCfg := SimConfig{
		Seed:                   cfg.Seed,
		BatchSize:              cfg.SimsPerPersonaPerCycle,
		Personas:               cfg.Personas,
		AgentID:                agentID,
		MaxTurnsPerAgent:       constants.DefaultSelfLearningConfig().DefaultMaxTurnsPerAgent,
		Judges:                 constants.DefaultSelfLearningConfig().Judges,
		MaxRunCostUSD:          cfg.MaxIncrementalCostUSD,
		BaseRunCostUSD:         s.baseline,
		MaxPromptIterations:    1,
		MetaEvalEveryJudgeRuns: cfg.MetaEvalEveryNJudges,
		PersonaGuidance:        PersonaGuidanceFromScores(agentID, bottom, nil),
	}
	_, simScores, err := RunSimulationScored(simCfg, simCfg.Judges)
	if err != nil {
		return err
	}
	judgeRuns := countJudgeRuns(simScores)
	s.addCycleResults(len(simScores), judgeRuns)

	// Meta-evaluation — non-fatal
	status := s.Status()
	if cfg.MetaEvalEveryNJudges > 0 && status.JudgeRunsSinceMeta >= cfg.MetaEvalEveryNJudges {
		if _, err := RunMetaEvaluation(agentID); err != nil {
			log.Printf("[eval] learning supervisor meta evaluation failed agent=%s err=%v (non-fatal)", agentID, err)
		}
		s.resetJudgeRuns()
	}

	// Prompt generation — non-fatal
	status = s.Status()
	if cfg.PromptGenEveryNScores > 0 && status.ScoresSinceGen >= cfg.PromptGenEveryNScores {
		exp, err := proposePromptForAgent(agentID, simCfg)
		if err != nil {
			log.Printf("[eval] learning supervisor prompt gen failed agent=%s err=%v (non-fatal)", agentID, err)
			return nil
		}
		if exp == nil {
			s.deferPromptGeneration()
			return nil
		}
		if exp.Adopted {
			s.resetScoresSinceGen()
			return nil
		}
		s.noteRejectedCandidate()
	}
	return nil
}

func proposePromptForAgent(agentID models.AgentID, cfg SimConfig) (*models.PromptExperiment, error) {
	current, err := collections.ActivePromptVersion(agentID)
	if err != nil {
		return nil, err
	}
	controlStats, err := lowScoringSimulationScores(agentID, 5)
	if err != nil {
		return nil, err
	}
	if !simulationScoresNeedPromptImprovement(controlStats) {
		return nil, nil
	}
	candidateVersion, err := nextPromptVersion(agentID)
	if err != nil {
		return nil, err
	}
	evidence := PromptGenerationEvidenceWithHistory(agentID, current.VersionNumber, candidateVersion, controlStats, nil)
	candidatePrompt, inputTokens, outputTokens, modelUsed, err := generateCandidatePrompt(agentID, current.PromptText, evidence)
	if err != nil {
		return nil, err
	}
	treatmentCfg := cfg
	treatmentCfg.AgentID = agentID
	treatmentCfg.PromptOverrides = map[models.AgentID]PromptOverride{
		agentID: {VersionNumber: candidateVersion, PromptText: candidatePrompt},
	}
	treatmentCfg.PersonaGuidance = PersonaGuidanceFromScores(agentID, controlStats, nil)
	_, treatmentStats, err := RunSimulationScored(treatmentCfg, cfg.Judges)
	if err != nil {
		return nil, err
	}
	return buildAndSavePromptExperiment(
		agentID,
		current.VersionNumber,
		candidateVersion,
		aggregateSimulationMeans(controlStats),
		aggregateComplianceRate(controlStats),
		controlStats,
		treatmentStats,
		candidatePrompt,
		modelUsed,
		inputTokens,
		outputTokens,
		cfg,
	)
}

func lowScoringSimulationScores(agentID models.AgentID, limit int) ([]SimulationScore, error) {
	if limit <= 0 {
		limit = 5
	}
	rows, err := scoresForAgent(agentID)
	if err != nil {
		return nil, err
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CompositeScore == rows[j].CompositeScore {
			return rows[i].CreatedAt.After(rows[j].CreatedAt)
		}
		return rows[i].CompositeScore < rows[j].CompositeScore
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	out := make([]SimulationScore, 0, len(rows))
	for _, row := range rows {
		out = append(out, simulationScoreFromRow(row))
	}
	return out, nil
}

func simulationScoreFromRow(row models.ConversationScore) SimulationScore {
	var judges []JudgeResult
	if row.ComplianceBreakdown != nil {
		data, _ := json.Marshal(row.ComplianceBreakdown["judge_results"])
		_ = json.Unmarshal(data, &judges)
	}
	rate := 0.0
	if row.CompliancePassed != nil && *row.CompliancePassed {
		rate = 1
	}
	return SimulationScore{
		SimulationSeed:    derefString(row.Seed),
		Persona:           derefPersona(row.PersonaType),
		WorkflowID:        derefString(row.WorkflowId),
		ConversationID:    row.ConversationId,
		PromptVersion:     row.PromptVersion,
		Scores:            []float64{row.CompositeScore},
		Mean:              row.CompositeScore,
		ComplianceRate:    rate,
		JudgeDisagreement: derefFloat(row.JudgeDisagreementDelta),
		JudgeResults:      judges,
	}
}

func simulationScoresNeedPromptImprovement(scores []SimulationScore) bool {
	if len(scores) == 0 {
		return false
	}
	for _, score := range scores {
		if score.ComplianceRate < 1 || score.Mean < 75 || score.JudgeDisagreement > constants.DefaultSelfLearningConfig().MaxJudgeDisagreement {
			return true
		}
	}
	return false
}

func normalizeSupervisorConfig(cfg SupervisorConfig) SupervisorConfig {
	appCfg := constants.AppCfg.Get()
	if len(cfg.AgentsRotation) == 0 {
		cfg.AgentsRotation = []models.AgentID{models.AgentAria, models.AgentNova, models.AgentDelta}
	}
	if len(cfg.Personas) == 0 {
		cfg.Personas = defaultPersonas()
	}
	if cfg.SimsPerPersonaPerCycle <= 0 {
		cfg.SimsPerPersonaPerCycle = 3
	}
	if cfg.PromptGenEveryNScores <= 0 {
		cfg.PromptGenEveryNScores = 3
	}
	if cfg.MetaEvalEveryNJudges <= 0 {
		cfg.MetaEvalEveryNJudges = 4
	}
	if cfg.MaxIncrementalCostUSD <= 0 {
		cfg.MaxIncrementalCostUSD = appCfg.LearningLoopBudgetUSD
	}
	if cfg.MaxRunDuration <= 0 {
		cfg.MaxRunDuration = 4 * time.Hour
	}
	if cfg.Seed == 0 {
		cfg.Seed = time.Now().UTC().Unix()
	}
	return cfg
}

func (s *Supervisor) config() SupervisorConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

func (s *Supervisor) startedAt() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status.StartedAt
}

func (s *Supervisor) setCurrentAgent(agentID models.AgentID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.CurrentAgent = agentID
	s.status.LastActivity = time.Now().UTC()
	s.status.LastError = ""
}

func (s *Supervisor) addCycleResults(scores int, judgeRuns int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Cycles++
	s.status.ScoresSinceGen += scores
	s.status.JudgeRunsSinceMeta += judgeRuns
	s.status.LastActivity = time.Now().UTC()
}

func (s *Supervisor) updateSpent(spent float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.SpentUSDIncremental = spent
	s.status.LastActivity = time.Now().UTC()
}

func (s *Supervisor) setError(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.LastError = strings.TrimSpace(msg)
	s.status.LastActivity = time.Now().UTC()
}

func (s *Supervisor) setStopReason(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.StopReason = reason
	s.status.LastActivity = time.Now().UTC()
}

func (s *Supervisor) resetJudgeRuns() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.JudgeRunsSinceMeta = 0
}

func (s *Supervisor) resetScoresSinceGen() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.ScoresSinceGen = 0
	s.status.RejectedCandidatesInRow = 0
}

func (s *Supervisor) noteRejectedCandidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.RejectedCandidatesInRow++
	if s.status.RejectedCandidatesInRow >= 3 {
		s.status.ScoresSinceGen = int(float64(s.cfg.PromptGenEveryNScores) * 1.5)
		s.status.RejectedCandidatesInRow = 0
	}
}

func (s *Supervisor) deferPromptGeneration() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.ScoresSinceGen = 0
}

func (s *Supervisor) sleepOrStop(d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-s.ctx.Done():
	case <-timer.C:
	}
}

func derefPersona(v *models.Persona) models.Persona {
	if v == nil {
		return ""
	}
	return *v
}

func (s *Supervisor) incrementConsecutiveErrors() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.ConsecutiveErrors++
}

func (s *Supervisor) resetConsecutiveErrors() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.ConsecutiveErrors = 0
}

func (s *Supervisor) consecutiveErrorCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status.ConsecutiveErrors
}
