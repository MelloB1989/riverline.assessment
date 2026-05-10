package eval

import (
	"fmt"
	"log"
	"sync"
	"time"

	"riverline_server/constants"
	"riverline_server/internal/collections"
	"riverline_server/internal/models"

	"github.com/MelloB1989/karma/utils"
	"github.com/MelloB1989/karma/v2/orm"
)

type FullCycleConfig struct {
	Seed                   int64                            `json:"seed"`
	BatchSize              int                              `json:"batch_size"`
	Agents                 []models.AgentID                 `json:"agents"`
	Personas               []models.Persona                 `json:"personas"`
	MaxTurnsPerAgent       int                              `json:"max_turns_per_agent"`
	Judges                 []constants.EvaluatorJudgeConfig `json:"judges"`
	MaxCostUSD             float64                          `json:"max_cost_usd"`
	MaxPromptIterations    int                              `json:"max_prompt_iterations"`
	MetaEvalEveryJudgeRuns int                              `json:"meta_eval_every_judge_runs"`
	Reset                  bool                             `json:"reset,omitempty"`
}

type FullCycleReport struct {
	Config        FullCycleConfig            `json:"config"`
	AgentReports  []AgentCycleReport         `json:"agent_reports"`
	CostBreakdown CostBreakdown              `json:"cost_breakdown"`
	PromptHistory []models.PromptVersion     `json:"prompt_history"`
	EvalHistory   []models.EvaluatorVersion  `json:"evaluator_history"`
	AllScores     []models.ConversationScore `json:"all_scores"`
	StartedAt     time.Time                  `json:"started_at"`
	CompletedAt   time.Time                  `json:"completed_at"`
	DurationSec   float64                    `json:"duration_sec"`
}

type AgentCycleReport struct {
	AgentID     models.AgentID   `json:"agent_id"`
	Experiment  ExperimentReport `json:"experiment"`
	MetaEval    MetaEvalReport   `json:"meta_eval"`
	Canaries    CanaryReport     `json:"canaries"`
	DurationSec float64          `json:"duration_sec"`
}

type ExperimentReport struct {
	Experiment         models.PromptExperiment         `json:"experiment"`
	ControlScores      []SimulationScore               `json:"control_scores"`
	TreatmentScores    []SimulationScore               `json:"treatment_scores"`
	ControlByPersona   map[models.Persona]PersonaStats `json:"control_by_persona"`
	TreatmentByPersona map[models.Persona]PersonaStats `json:"treatment_by_persona"`
	ControlByJudge     map[string]JudgeStats           `json:"control_by_judge"`
	TreatmentByJudge   map[string]JudgeStats           `json:"treatment_by_judge"`
	Adopted            bool                            `json:"adopted"`
	Decision           string                          `json:"decision"`
	StatSummary        StatSummary                     `json:"stat_summary"`
}

type PersonaStats struct {
	N              int     `json:"n"`
	Mean           float64 `json:"mean"`
	Stddev         float64 `json:"stddev"`
	ComplianceRate float64 `json:"compliance_rate"`
}

type JudgeStats struct {
	N              int     `json:"n"`
	Mean           float64 `json:"mean"`
	Stddev         float64 `json:"stddev"`
	ComplianceRate float64 `json:"compliance_rate"`
	MeanCostUSD    float64 `json:"mean_cost_usd"`
	MeanInputTok   float64 `json:"mean_input_tokens"`
	MeanOutputTok  float64 `json:"mean_output_tokens"`
}

type StatSummary struct {
	ControlMean             float64 `json:"control_mean"`
	ControlStddev           float64 `json:"control_stddev"`
	ControlMedian           float64 `json:"control_median"`
	ControlComplianceRate   float64 `json:"control_compliance_rate"`
	TreatmentMean           float64 `json:"treatment_mean"`
	TreatmentStddev         float64 `json:"treatment_stddev"`
	TreatmentMedian         float64 `json:"treatment_median"`
	TreatmentComplianceRate float64 `json:"treatment_compliance_rate"`
	MeanDelta               float64 `json:"mean_delta"`
	PValue                  float64 `json:"p_value"`
	CohensD                 float64 `json:"cohens_d"`
	IsSignificant           bool    `json:"is_significant"`
}

type MetaEvalReport struct {
	Flags         []models.MetaFlag `json:"flags"`
	FlagCount     int               `json:"flag_count"`
	ResolvedCount int               `json:"resolved_count"`
}

type CanaryReport struct {
	Results       []models.CanaryResult `json:"results"`
	TotalCanaries int                   `json:"total_canaries"`
	Passed        int                   `json:"passed"`
	Failed        int                   `json:"failed"`
}

type CostBreakdown struct {
	TotalUSD              float64            `json:"total_usd"`
	ByUsageType           map[string]float64 `json:"by_usage_type"`
	ByModel               map[string]float64 `json:"by_model"`
	TotalPromptTokens     int                `json:"total_prompt_tokens"`
	TotalCompletionTokens int                `json:"total_completion_tokens"`
}

var currentCostCache = struct {
	sync.Mutex
	loadedAt time.Time
	value    float64
}{}

func RunFullCycle(cfg FullCycleConfig) (*FullCycleReport, error) {
	start := time.Now()
	log.Printf("[eval] full cycle start agents=%v batch_size=%d personas=%d seed=%d", cfg.Agents, cfg.BatchSize, len(cfg.Personas), cfg.Seed)

	if err := collections.EnsureDefaults(); err != nil {
		return nil, fmt.Errorf("ensure defaults: %w", err)
	}

	if len(cfg.Agents) == 0 {
		cfg.Agents = []models.AgentID{models.AgentAria, models.AgentNova, models.AgentDelta}
	}
	if len(cfg.Personas) == 0 {
		cfg.Personas = defaultPersonas()
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = constants.DefaultSelfLearningConfig().DefaultBatchSize
	}
	baseCost, err := currentTotalCostUSD()
	if err != nil {
		return nil, fmt.Errorf("load initial cost: %w", err)
	}

	report := &FullCycleReport{
		Config:    cfg,
		StartedAt: start,
	}

	for _, agentID := range cfg.Agents {
		if cfg.MaxCostUSD > 0 {
			currentCost, err := currentTotalCostUSD()
			if err != nil {
				return nil, err
			}
			if spent := currentCost - baseCost; spent >= cfg.MaxCostUSD {
				return nil, fmt.Errorf("eval run cost budget exceeded before agent %s: spent=$%.4f budget=$%.4f", agentID, spent, cfg.MaxCostUSD)
			}
		}
		agentStart := time.Now()
		log.Printf("[eval] full cycle agent start agent=%s", agentID)

		agentReport, err := runAgentCycle(agentID, cfg, baseCost)
		if err != nil {
			return nil, fmt.Errorf("agent cycle %s: %w", agentID, err)
		}
		agentReport.DurationSec = time.Since(agentStart).Seconds()
		report.AgentReports = append(report.AgentReports, *agentReport)

		log.Printf("[eval] full cycle agent done agent=%s adopted=%t meta_flags=%d canaries_passed=%d/%d duration=%.1fs",
			agentID, agentReport.Experiment.Adopted, agentReport.MetaEval.FlagCount,
			agentReport.Canaries.Passed, agentReport.Canaries.TotalCanaries, agentReport.DurationSec)
	}

	costBreakdown, err := loadCostBreakdown()
	if err != nil {
		return nil, fmt.Errorf("load cost breakdown: %w", err)
	}
	report.CostBreakdown = *costBreakdown

	promptHistory, err := loadAllPromptVersions()
	if err != nil {
		return nil, fmt.Errorf("load prompt versions: %w", err)
	}
	report.PromptHistory = promptHistory

	evalHistory, err := loadAllEvaluatorVersions()
	if err != nil {
		return nil, fmt.Errorf("load evaluator versions: %w", err)
	}
	report.EvalHistory = evalHistory

	allScores, err := loadAllScores()
	if err != nil {
		return nil, fmt.Errorf("load all scores: %w", err)
	}
	report.AllScores = allScores

	report.CompletedAt = time.Now()
	report.DurationSec = time.Since(start).Seconds()

	log.Printf("[eval] full cycle done agents=%d total_scores=%d total_cost=$%.4f duration=%.1fs",
		len(cfg.Agents), len(report.AllScores), report.CostBreakdown.TotalUSD, report.DurationSec)

	return report, nil
}

func runAgentCycle(agentID models.AgentID, cfg FullCycleConfig, baseCost float64) (*AgentCycleReport, error) {
	simCfg := SimConfig{
		Seed:                   cfg.Seed,
		BatchSize:              cfg.BatchSize,
		AgentID:                agentID,
		Personas:               cfg.Personas,
		MaxTurnsPerAgent:       cfg.MaxTurnsPerAgent,
		Judges:                 cfg.Judges,
		MaxRunCostUSD:          cfg.MaxCostUSD,
		BaseRunCostUSD:         baseCost,
		MaxPromptIterations:    cfg.MaxPromptIterations,
		MetaEvalEveryJudgeRuns: cfg.MetaEvalEveryJudgeRuns,
	}

	exp, controlScores, treatmentScores, err := runImprovementCycleDetailed(agentID, simCfg)
	if err != nil {
		return nil, fmt.Errorf("improvement cycle: %w", err)
	}

	expReport := ExperimentReport{
		Experiment:         *exp,
		ControlScores:      controlScores,
		TreatmentScores:    treatmentScores,
		ControlByPersona:   groupByPersona(controlScores),
		TreatmentByPersona: groupByPersona(treatmentScores),
		ControlByJudge:     groupByJudge(controlScores),
		TreatmentByJudge:   groupByJudge(treatmentScores),
		Adopted:            exp.Adopted,
		Decision:           experimentDecision(exp),
		StatSummary: StatSummary{
			ControlMean:             exp.ControlMean,
			ControlStddev:           exp.ControlStddev,
			ControlMedian:           exp.ControlMedian,
			ControlComplianceRate:   exp.ControlComplianceRate,
			TreatmentMean:           exp.TreatmentMean,
			TreatmentStddev:         exp.TreatmentStddev,
			TreatmentMedian:         exp.TreatmentMedian,
			TreatmentComplianceRate: exp.TreatmentComplianceRate,
			MeanDelta:               exp.MeanDelta,
			PValue:                  exp.PValue,
			CohensD:                 derefFloat(exp.CohensD),
			IsSignificant:           derefBool(exp.IsSignificant),
		},
	}

	var flags []models.MetaFlag
	flags, err = RunMetaEvaluation(agentID)
	if err != nil {
		log.Printf("[eval] meta evaluation failed in agent cycle agent=%s err=%v (non-fatal, continuing)", agentID, err)
		flags = nil
	}
	resolved := 0
	for _, flag := range flags {
		if derefBool(flag.Resolved) {
			resolved++
		}
	}
	metaReport := MetaEvalReport{
		Flags:         flags,
		FlagCount:     len(flags),
		ResolvedCount: resolved,
	}

	var canaryResults []models.CanaryResult
	canaryResults, err = RunCanarySetForAgent(agentID)
	if err != nil {
		log.Printf("[eval] canary set failed in agent cycle agent=%s err=%v (non-fatal, continuing)", agentID, err)
		canaryResults = nil
	}
	passed := 0
	failed := 0
	for _, result := range canaryResults {
		if derefBool(result.CorrectlyFlagged) {
			passed++
		} else {
			failed++
		}
	}
	canaryReport := CanaryReport{
		Results:       canaryResults,
		TotalCanaries: len(canaryResults),
		Passed:        passed,
		Failed:        failed,
	}

	return &AgentCycleReport{
		AgentID:    agentID,
		Experiment: expReport,
		MetaEval:   metaReport,
		Canaries:   canaryReport,
	}, nil
}

func runImprovementCycleDetailed(agentID models.AgentID, cfg SimConfig) (*models.PromptExperiment, []SimulationScore, []SimulationScore, error) {
	start := time.Now()
	if err := collections.EnsureDefaults(); err != nil {
		return nil, nil, nil, err
	}
	current, err := collections.ActivePromptVersion(agentID)
	if err != nil {
		return nil, nil, nil, err
	}
	maxIterations := cfg.MaxPromptIterations
	if maxIterations <= 0 {
		maxIterations = constants.DefaultSelfLearningConfig().MaxPromptIterations
	}
	metaEvery := cfg.MetaEvalEveryJudgeRuns
	if metaEvery <= 0 {
		metaEvery = constants.DefaultSelfLearningConfig().MetaEvalEveryJudgeRuns
	}

	log.Printf("[eval] experiment loop start agent=%s control_version=%d max_iterations=%d batch_size=%d personas=%d", agentID, current.VersionNumber, maxIterations, cfg.BatchSize, len(cfg.Personas))

	cfg.AgentID = agentID
	cfg.PromptOverrides = nil
	controlStats, err := lowScoringSimulationScores(agentID, cfg.BatchSize*len(cfg.Personas))
	if err != nil || len(controlStats) == 0 {
		log.Printf("[eval] no existing scores found, simulating control...")
		_, controlStats, err = RunSimulationScored(cfg, cfg.Judges)
		if err != nil {
			return nil, nil, nil, err
		}
	} else {
		log.Printf("[eval] using %d existing low-scoring runs as control", len(controlStats))
	}
	controlScores := aggregateSimulationMeans(controlStats)
	controlCompliance := aggregateComplianceRate(controlStats)

	rejectedStats := []SimulationScore{}
	var finalExp *models.PromptExperiment
	var finalTreatment []SimulationScore
	var finalErr error
	for iteration := 1; iteration <= maxIterations; iteration++ {
		if err := enforceRunBudget(cfg); err != nil {
			return finalExp, controlStats, finalTreatment, err
		}
		candidateVersion, err := nextPromptVersion(agentID)
		if err != nil {
			return nil, nil, nil, err
		}
		log.Printf("[eval] experiment iteration start agent=%s iteration=%d/%d candidate_version=%d", agentID, iteration, maxIterations, candidateVersion)
		evidence := PromptGenerationEvidenceWithHistory(agentID, current.VersionNumber, candidateVersion, controlStats, rejectedStats)
		candidatePrompt, inputTokens, outputTokens, modelUsed, err := generateCandidatePrompt(agentID, current.PromptText, evidence)
		if err != nil {
			return nil, nil, nil, err
		}
		log.Printf("[eval] candidate prompt generated agent=%s candidate_version=%d iteration=%d tokens_in=%d tokens_out=%d model=%s prompt_chars=%d evidence_chars=%d", agentID, candidateVersion, iteration, inputTokens, outputTokens, modelUsed, len(candidatePrompt), len(evidence))

		treatmentCfg := cfg
		treatmentCfg.PromptOverrides = map[models.AgentID]PromptOverride{
			agentID: {VersionNumber: candidateVersion, PromptText: candidatePrompt},
		}
		treatmentCfg.PersonaGuidance = PersonaGuidanceFromScores(agentID, controlStats, rejectedStats)
		_, treatmentStats, err := RunSimulationScored(treatmentCfg, cfg.Judges)
		if err != nil {
			return nil, nil, nil, err
		}

		exp, err := buildAndSavePromptExperiment(agentID, current.VersionNumber, candidateVersion, controlScores, controlCompliance, controlStats, treatmentStats, candidatePrompt, modelUsed, inputTokens, outputTokens, cfg)
		if err != nil {
			return nil, nil, nil, err
		}
		finalExp = exp
		finalTreatment = treatmentStats
		log.Printf("[eval] experiment iteration done agent=%s experiment=%s iteration=%d delta=%.2f p=%.4f d=%.2f adopted=%t duration=%s",
			agentID, exp.Id, iteration, exp.MeanDelta, exp.PValue, derefFloat(exp.CohensD), exp.Adopted, time.Since(start))
		if exp.Adopted {
			return exp, controlStats, treatmentStats, nil
		}
		rejectedStats = append(rejectedStats, treatmentStats...)
		finalErr = fmt.Errorf("candidate rejected after iteration %d/%d: %s", iteration, maxIterations, derefString(exp.RejectionReason))
	}
	if finalExp == nil {
		return nil, controlStats, nil, finalErr
	}
	log.Printf("[eval] experiment loop exhausted agent=%s final_experiment=%s adopted=%t err=%v duration=%s", agentID, finalExp.Id, finalExp.Adopted, finalErr, time.Since(start))
	return finalExp, controlStats, finalTreatment, nil
}

func buildAndSavePromptExperiment(agentID models.AgentID, controlVersion int, candidateVersion int, controlScores []float64, controlCompliance float64, controlStats []SimulationScore, treatmentStats []SimulationScore, candidatePrompt string, modelUsed string, inputTokens int, outputTokens int, cfg SimConfig) (*models.PromptExperiment, error) {
	treatmentScores := aggregateSimulationMeans(treatmentStats)
	treatmentCompliance := aggregateComplianceRate(treatmentStats)
	slCfg := constants.DefaultSelfLearningConfig()
	pValue := WelchTTest(controlScores, treatmentScores)
	delta := Mean(treatmentScores) - Mean(controlScores)
	effectSize := CohensD(controlScores, treatmentScores)
	isSignificant := pValue < slCfg.AdoptionPValue && effectSize >= slCfg.AdoptionMinCohensD
	issueGatePassed, issueGateReason := targetedIssueGate(controlStats, treatmentStats)
	adopt := isSignificant &&
		delta >= slCfg.AdoptionMinMeanDelta &&
		Stddev(treatmentScores) <= slCfg.AdoptionMaxStddev &&
		treatmentCompliance >= slCfg.MinComplianceRate &&
		treatmentCompliance >= controlCompliance &&
		issueGatePassed
	rejection := rejectionReason(adopt, pValue, delta, effectSize, controlCompliance, treatmentCompliance, issueGatePassed, issueGateReason)
	exp := &models.PromptExperiment{
		Id:                      utils.GenerateID(),
		ControlVersion:          controlVersion,
		CandidateVersion:        candidateVersion,
		ControlN:                len(controlScores),
		ControlMean:             Mean(controlScores),
		ControlStddev:           Stddev(controlScores),
		ControlMedian:           ComputePercentile(controlScores, 50),
		ControlComplianceRate:   controlCompliance,
		ControlScores:           controlScores,
		TreatmentN:              len(treatmentScores),
		TreatmentMean:           Mean(treatmentScores),
		TreatmentStddev:         Stddev(treatmentScores),
		TreatmentMedian:         ComputePercentile(treatmentScores, 50),
		TreatmentComplianceRate: treatmentCompliance,
		TreatmentScores:         treatmentScores,
		MeanDelta:               delta,
		PValue:                  pValue,
		CohensD:                 floatPtr(effectSize),
		IsSignificant:           &isSignificant,
		Adopted:                 adopt,
		RejectionReason:         rejection,
		ExperimentCostUsd:       floatPtr(0),
		CreatedAt:               time.Now().UTC(),
	}
	expOrm := orm.Load(&models.PromptExperiment{})
	defer expOrm.Close()
	if err := expOrm.Insert(exp); err != nil {
		return nil, err
	}
	if err := saveCandidatePrompt(agentID, candidateVersion, candidatePrompt, adopt, exp); err != nil {
		return nil, err
	}
	if err := collections.LogCost("prompt_generation", &agentID, modelUsed, inputTokens, outputTokens, nil, &exp.Id); err != nil {
		return nil, err
	}
	return exp, nil
}

func countJudgeRuns(stats []SimulationScore) int {
	total := 0
	for _, score := range stats {
		total += len(score.JudgeResults)
	}
	return total
}

func maybeRunMetaEvaluationByJudgeBudget(agentID models.AgentID, cfg SimConfig, judgeRunsSinceMeta *int, threshold int) error {
	if threshold <= 0 || judgeRunsSinceMeta == nil || *judgeRunsSinceMeta < threshold {
		return nil
	}
	log.Printf("[eval] meta evaluation triggered agent=%s judge_runs_since_meta=%d threshold=%d", agentID, *judgeRunsSinceMeta, threshold)
	_, err := RunMetaEvaluation(agentID)
	if err != nil {
		log.Printf("[eval] meta evaluation failed agent=%s err=%v (non-fatal, resetting counter and continuing)", agentID, err)
	}
	*judgeRunsSinceMeta = 0
	return nil
}

func experimentDecision(exp *models.PromptExperiment) string {
	if exp.Adopted {
		return fmt.Sprintf("ADOPTED: v%d → v%d (delta=%.2f, p=%.4f, d=%.2f)",
			exp.ControlVersion, exp.CandidateVersion, exp.MeanDelta, exp.PValue, derefFloat(exp.CohensD))
	}
	reason := "did not pass adoption gates"
	if exp.RejectionReason != nil {
		reason = *exp.RejectionReason
	}
	return fmt.Sprintf("REJECTED: v%d kept (%s)", exp.ControlVersion, reason)
}

func groupByPersona(scores []SimulationScore) map[models.Persona]PersonaStats {
	groups := map[models.Persona][]SimulationScore{}
	for _, score := range scores {
		groups[score.Persona] = append(groups[score.Persona], score)
	}
	result := map[models.Persona]PersonaStats{}
	for persona, group := range groups {
		means := make([]float64, len(group))
		complianceSum := 0.0
		for i, s := range group {
			means[i] = s.Mean
			complianceSum += s.ComplianceRate
		}
		result[persona] = PersonaStats{
			N:              len(group),
			Mean:           Mean(means),
			Stddev:         Stddev(means),
			ComplianceRate: complianceSum / float64(len(group)),
		}
	}
	return result
}

func groupByJudge(scores []SimulationScore) map[string]JudgeStats {
	type row struct {
		score        float64
		compliance   bool
		cost         float64
		inputTokens  int
		outputTokens int
	}
	groups := map[string][]row{}
	for _, simScore := range scores {
		for _, judge := range simScore.JudgeResults {
			key := judge.Name
			if key == "" {
				key = judge.ModelUsed
			}
			groups[key] = append(groups[key], row{
				score:        judge.Metrics.CompositeScore,
				compliance:   judge.Metrics.CompliancePass > 0,
				cost:         judge.CostUSD,
				inputTokens:  judge.InputTokens,
				outputTokens: judge.OutputTokens,
			})
		}
	}
	out := map[string]JudgeStats{}
	for key, rows := range groups {
		values := make([]float64, 0, len(rows))
		costs := make([]float64, 0, len(rows))
		inputs := make([]float64, 0, len(rows))
		outputs := make([]float64, 0, len(rows))
		compliance := 0
		for _, row := range rows {
			values = append(values, row.score)
			costs = append(costs, row.cost)
			inputs = append(inputs, float64(row.inputTokens))
			outputs = append(outputs, float64(row.outputTokens))
			if row.compliance {
				compliance++
			}
		}
		out[key] = JudgeStats{
			N:              len(rows),
			Mean:           Mean(values),
			Stddev:         Stddev(values),
			ComplianceRate: float64(compliance) / float64(len(rows)),
			MeanCostUSD:    Mean(costs),
			MeanInputTok:   Mean(inputs),
			MeanOutputTok:  Mean(outputs),
		}
	}
	return out
}

func loadCostBreakdown() (*CostBreakdown, error) {
	o := orm.Load(&models.LlmCostLog{})
	defer o.Close()
	var costs []models.LlmCostLog
	if err := o.GetAll().Scan(&costs); err != nil {
		return nil, err
	}
	breakdown := &CostBreakdown{
		ByUsageType: map[string]float64{},
		ByModel:     map[string]float64{},
	}
	for _, cost := range costs {
		breakdown.TotalUSD += cost.CostUsd
		breakdown.ByUsageType[cost.CallType] += cost.CostUsd
		breakdown.ByModel[cost.ModelUsed] += cost.CostUsd
		breakdown.TotalPromptTokens += cost.PromptTokens
		breakdown.TotalCompletionTokens += cost.CompletionTokens
	}
	return breakdown, nil
}

func currentTotalCostUSD() (float64, error) {
	currentCostCache.Lock()
	if time.Since(currentCostCache.loadedAt) < 5*time.Second {
		value := currentCostCache.value
		currentCostCache.Unlock()
		return value, nil
	}
	currentCostCache.Unlock()

	breakdown, err := loadCostBreakdown()
	if err != nil {
		return 0, err
	}
	currentCostCache.Lock()
	currentCostCache.value = breakdown.TotalUSD
	currentCostCache.loadedAt = time.Now()
	currentCostCache.Unlock()
	return breakdown.TotalUSD, nil
}

func IncrementalSpentUSD(baseline float64) (float64, error) {
	total, err := currentTotalCostUSD()
	if err != nil {
		return 0, err
	}
	spent := total - baseline
	if spent < 0 {
		return 0, nil
	}
	return spent, nil
}

func loadAllPromptVersions() ([]models.PromptVersion, error) {
	o := orm.Load(&models.PromptVersion{})
	defer o.Close()
	var rows []models.PromptVersion
	if err := o.GetAll().Scan(&rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func loadAllEvaluatorVersions() ([]models.EvaluatorVersion, error) {
	o := orm.Load(&models.EvaluatorVersion{})
	defer o.Close()
	var rows []models.EvaluatorVersion
	if err := o.GetAll().Scan(&rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func loadAllScores() ([]models.ConversationScore, error) {
	o := orm.Load(&models.ConversationScore{})
	defer o.Close()
	var rows []models.ConversationScore
	if err := o.GetAll().Scan(&rows); err != nil {
		return nil, err
	}
	return rows, nil
}
