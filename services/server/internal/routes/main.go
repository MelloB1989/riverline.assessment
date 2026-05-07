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
}
