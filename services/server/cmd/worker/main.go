package main

import (
	"log"

	"riverline_server/constants"
	"riverline_server/internal/workflows"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

func main() {
	c, err := client.Dial(client.Options{HostPort: constants.AppCfg.Get().TemporalHostPort})
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	w := worker.New(c, workflows.BorrowerCollectionsTaskQueue, worker.Options{})
	w.RegisterWorkflow(workflows.BorrowerCollectionsWorkflow)
	w.RegisterActivity(workflows.RunARIA)
	w.RegisterActivity(workflows.CompleteARIA)
	w.RegisterActivity(workflows.StartNOVA)
	w.RegisterActivity(workflows.CompleteNOVA)
	w.RegisterActivity(workflows.SendNOVAOfferEmail)
	w.RegisterActivity(workflows.SendDELTAFinalOfferEmail)
	w.RegisterActivity(workflows.CompleteDELTA)

	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatal(err)
	}
}
