package routes

import (
	"riverline_server/internal/handlers"
	"riverline_server/internal/middleware"

	"github.com/gofiber/fiber/v2"
)

func SetupMainRoutes(v1 fiber.Router) {
	v1.Post("/vapi/webhook", handlers.VapiWebhook)

	protected := v1.Group("", middleware.ClerkAuth())
	protected.Post("/chat/:workflowId", handlers.PostChat)
	protected.Get("/chat/:workflowId/stream", handlers.StreamChat)
	protected.Post("/workflows/start", handlers.StartWorkflow)
	protected.Get("/workflows/:id", handlers.GetWorkflow)
	protected.Get("/conversations/:id", handlers.GetConversation)
	protected.Get("/workflows/:workflowId/delta-handoff", handlers.GetDeltaHandoff)
	protected.Get("/workflows/:workflowId/delta-handoff.pdf", handlers.GetDeltaHandoffPDF)

	admin := protected.Group("/admin", middleware.RequireAdmin())
	admin.Get("/eval", handlers.AdminEvalSummary)
	admin.Post("/simulations", handlers.AdminRunSimulations)
	admin.Post("/prompt-experiments", handlers.AdminRunPromptExperiment)
	admin.Get("/eval/experiments/:id", handlers.AdminExperimentDetail)
	admin.Post("/evaluations/rerun", handlers.AdminRerunEvaluations)
	admin.Post("/prompt-versions/rollback", handlers.AdminRollbackPrompt)
	admin.Post("/meta-evaluations", handlers.AdminRunMetaEvaluation)
	admin.Post("/eval/full-cycle", handlers.AdminRunFullCycle)
	admin.Post("/eval/full-cycle/start", handlers.AdminStartFullCycle)
	admin.Get("/eval/runs/:id", handlers.AdminEvalProgress)
	admin.Post("/simulations/single", handlers.AdminSingleSimulation)
	admin.Post("/learning/start", handlers.AdminLearningStart)
	admin.Post("/learning/stop", handlers.AdminLearningStop)
	admin.Get("/learning/status", handlers.AdminLearningStatus)
	admin.Post("/eval/reset", handlers.AdminResetAndReseed)
	admin.Get("/eval/metrics", handlers.AdminEvalMetrics)
	admin.Get("/eval/meta", handlers.AdminEvalMeta)
	admin.Get("/eval/export/scores", handlers.AdminExportScoresCSV)
	admin.Get("/eval/export/experiments", handlers.AdminExportExperimentsCSV)
}
