package routes

import (
	"riverline_server/internal/handlers"

	"github.com/gofiber/fiber/v2"
)

func SetupMainRoutes(v1 fiber.Router) {
	v1.Post("/chat/:workflowId", handlers.PostChat)
	v1.Get("/chat/:workflowId/stream", handlers.StreamChat)
	v1.Post("/vapi/webhook", handlers.VapiWebhook)
	v1.Post("/workflows/start", handlers.StartWorkflow)
	v1.Get("/workflows/:id", handlers.GetWorkflow)
	v1.Get("/conversations/:id", handlers.GetConversation)
	v1.Get("/admin/eval", handlers.AdminEvalSummary)
}
