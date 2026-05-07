package main

import (
	"context"
	"log"

	"riverline_server/internal/collections"
	"riverline_server/internal/temporalclient"
	"riverline_server/internal/workflows"

	"go.temporal.io/sdk/worker"
)

func main() {
	if err := collections.EnsureDefaults(); err != nil {
		log.Printf("bootstrap defaults skipped: %v", err)
	}
	if err := collections.SyncNovaVapiAssistant(context.Background()); err != nil {
		log.Printf("vapi nova assistant sync skipped: %v", err)
	}

	c, err := temporalclient.Dial()
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	w := worker.New(c, workflows.BorrowerCollectionsTaskQueue, worker.Options{})
	w.RegisterWorkflow(workflows.AriaHandoffWorkflow)
	w.RegisterWorkflow(workflows.NovaCompletionWorkflow)
	w.RegisterWorkflow(workflows.DeltaHandoffWorkflow)
	w.RegisterWorkflow(workflows.EvaluationWorkflow)
	w.RegisterActivity(workflows.CompleteARIA)
	w.RegisterActivity(workflows.PrepareNOVA)
	w.RegisterActivity(workflows.GetNovaScheduledCallAt)
	w.RegisterActivity(workflows.StartNOVA)
	w.RegisterActivity(workflows.PollNOVACompletionFromVapi)
	w.RegisterActivity(workflows.CompleteNOVA)
	w.RegisterActivity(workflows.SendNOVAOfferEmail)
	w.RegisterActivity(workflows.SendDELTAFinalOfferEmail)
	w.RegisterActivity(workflows.EvaluateWorkflowConversations)

	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatal(err)
	}
}
