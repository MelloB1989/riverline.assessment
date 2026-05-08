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
	protected.Get("/admin/eval", handlers.AdminEvalSummary)
	protected.Post("/admin/simulations", handlers.AdminRunSimulations)
	protected.Post("/admin/prompt-experiments", handlers.AdminRunPromptExperiment)
	protected.Get("/admin/eval/experiments/:id", handlers.AdminExperimentDetail)
	protected.Post("/admin/evaluations/rerun", handlers.AdminRerunEvaluations)
	protected.Post("/admin/prompt-versions/rollback", handlers.AdminRollbackPrompt)
	protected.Post("/admin/meta-evaluations", handlers.AdminRunMetaEvaluation)
	protected.Post("/admin/eval/full-cycle", handlers.AdminRunFullCycle)
	protected.Get("/admin/eval/metrics", handlers.AdminEvalMetrics)
	protected.Get("/admin/eval/meta", handlers.AdminEvalMeta)
}
