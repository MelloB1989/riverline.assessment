package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"riverline_server/internal/collections"
	rivereval "riverline_server/internal/eval"
	"riverline_server/internal/models"

	"github.com/MelloB1989/karma/v2/orm"
)

func main() {
	seed := flag.Int64("seed", 42, "simulation seed")
	batchSize := flag.Int("batch-size", 1, "batch size per persona")
	agent := flag.String("agent", "all", "aria, nova, delta, or all")
	output := flag.String("output", "./eval-artifacts", "output directory for reproducible raw JSON artifacts")
	flag.Parse()

	if err := collections.EnsureDefaults(); err != nil {
		log.Fatal(err)
	}
	agents := []models.AgentID{models.AgentAria, models.AgentNova, models.AgentDelta}
	if *agent != "all" {
		agents = []models.AgentID{models.AgentID(strings.ToLower(*agent))}
	}
	personas := []models.Persona{models.PersonaCooperative, models.PersonaCombative, models.PersonaEvasive, models.PersonaDistressed, models.PersonaConfused}
	if *batchSize >= 10 {
		perAgent := len(personas) * *batchSize * 2
		log.Printf("large eval run: each agent will run about %d full-flow simulations before meta/canary scoring; use --batch-size=1 for smoke tests", perAgent)
	}
	totalConversations := 0
	for _, agentID := range agents {
		log.Printf("running prompt experiment for %s with seed=%d batch_size=%d personas=%d", agentID, *seed, *batchSize, len(personas))
		cfg := rivereval.SimConfig{Seed: *seed, BatchSize: *batchSize, AgentID: agentID, Personas: personas}
		exp, err := rivereval.RunImprovementCycle(agentID, cfg)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("running meta evaluation for %s", agentID)
		flags, err := rivereval.RunMetaEvaluation(agentID)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("running canaries after %s", agentID)
		canaries, err := rivereval.RunCanarySetForAgent(agentID)
		if err != nil {
			log.Fatal(err)
		}
		totalConversations += exp.ControlN + exp.TreatmentN
		fmt.Printf("%s: scored=%d control_mean=%.2f treatment_mean=%.2f experiment_delta=%.2f adopted=%t meta_flags=%d canaries=%d\n", agentID, exp.ControlN+exp.TreatmentN, exp.ControlMean, exp.TreatmentMean, exp.MeanDelta, exp.Adopted, len(flags), len(canaries))
	}
	fmt.Printf("total_scored=%d seed=%d batch_size=%d\n", totalConversations, *seed, *batchSize)
	if err := writeArtifacts(*output, *seed, *batchSize, *agent); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("artifacts_written=%s\n", *output)
}

func writeArtifacts(output string, seed int64, batchSize int, agent string) error {
	if err := os.MkdirAll(output, 0o755); err != nil {
		return err
	}
	runConfig := map[string]any{"seed": seed, "batch_size": batchSize, "agent": agent}
	if err := writeJSON(filepath.Join(output, "run_config.json"), runConfig); err != nil {
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
	if err := writeTable[models.PromptExperiment](filepath.Join(output, "prompt_experiments.json"), &models.PromptExperiment{}); err != nil {
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
	if err := writeTable[models.PromptVersion](filepath.Join(output, "prompt_versions.json"), &models.PromptVersion{}); err != nil {
		return err
	}
	return nil
}

func writeTable[T any](path string, model any) error {
	o := orm.Load(model)
	defer o.Close()
	var rows []T
	if err := o.GetAll().Scan(&rows); err != nil {
		return err
	}
	return writeJSON(path, rows)
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
