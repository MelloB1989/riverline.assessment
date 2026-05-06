package cmd

import (
	"log"
	"riverline_server/constants"
	"riverline_server/internal/routes"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

func StartServer() {
	port := constants.AppCfg.Get().Port
	appServer := createFiberApp()
	appServer.Use("/health", healthCheckHandler())
	v1 := appServer.Group("/v1")
	routes.SetupMainRoutes(v1)
	if err := appServer.Listen(":" + port); err != nil {
		log.Fatal(err)
	}
}

func healthCheckHandler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status":  "healthy",
			"service": "riverline",
		})
	}
}

func createFiberApp() *fiber.App {
	return fiber.New(fiber.Config{
		AppName:               "Riverline",
		DisableStartupMessage: true,
	})
}

func setupGlobalMiddleware(app *fiber.App) {
	app.Use(logger.New())
	app.Use(recover.New(recover.Config{
		EnableStackTrace: true,
	}))
	app.Use(cors.New(cors.Config{
		AllowOrigins:     strings.Join(constants.GetAllowedOrigins(), ","),
		AllowHeaders:     "Origin, Content-Type, Accept, Authorization",
		AllowMethods:     "GET, POST, PUT, PATCH, DELETE, OPTIONS",
		AllowCredentials: true,
	}))
	app.Use(securityHeaders())
}

func securityHeaders() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("X-Frame-Options", "DENY")
		c.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Set("X-XSS-Protection", "0")
		c.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		c.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'")
		c.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		c.Set("Cache-Control", "no-store")
		return c.Next()
	}
}
