package main

import (
	"flag"
	"fmt"
	"log"
	"strings"

	"riverline_server/internal/collections"
	rivereval "riverline_server/internal/eval"
	"riverline_server/internal/models"
)

func main() {
	seed := flag.Int64("seed", 42, "simulation seed")
	batchSize := flag.Int("batch-size", 20, "batch size per persona")
	agent := flag.String("agent", "all", "aria, nova, delta, or all")
	flag.Parse()

	if err := collections.EnsureDefaults(); err != nil {
		log.Fatal(err)
	}
	agents := []models.AgentID{models.AgentAria, models.AgentNova, models.AgentDelta}
	if *agent != "all" {
		agents = []models.AgentID{models.AgentID(strings.ToLower(*agent))}
	}
	personas := []models.Persona{models.PersonaCooperative, models.PersonaCombative, models.PersonaEvasive, models.PersonaDistressed, models.PersonaConfused}
	totalConversations := 0
	for _, agentID := range agents {
		cfg := rivereval.SimConfig{Seed: *seed, BatchSize: *batchSize, AgentID: agentID, Personas: personas}
		convos, err := rivereval.RunSimulation(cfg)
		if err != nil {
			log.Fatal(err)
		}
		scores, err := rivereval.ScoreAll(convos)
		if err != nil {
			log.Fatal(err)
		}
		exp, err := rivereval.RunImprovementCycle(agentID, cfg)
		if err != nil {
			log.Fatal(err)
		}
		flags, err := rivereval.RunMetaEvaluation(agentID)
		if err != nil {
			log.Fatal(err)
		}
		canaries, err := rivereval.RunCanarySet(1)
		if err != nil {
			log.Fatal(err)
		}
		totalConversations += len(scores)
		fmt.Printf("%s: scored=%d mean=%.2f experiment_delta=%.2f adopted=%t meta_flags=%d canaries=%d\n", agentID, len(scores), rivereval.Mean(scores), exp.MeanDelta, exp.Adopted, len(flags), len(canaries))
	}
	fmt.Printf("total_scored=%d seed=%d batch_size=%d\n", totalConversations, *seed, *batchSize)
}
