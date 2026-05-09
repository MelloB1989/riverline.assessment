package middleware

import (
	"fmt"
	"os"
	"riverline_server/internal/models"
	"strings"
	"time"

	"github.com/MelloB1989/karma/v2/orm"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

func ClerkAuth() fiber.Handler {
	return func(c *fiber.Ctx) error {
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Missing authorization header",
			})
		}

		// Extract token from Bearer header
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == authHeader {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Invalid authorization header format",
			})
		}

		// Verify the token using JWT_SECRET
		claims, err := verifyClerkToken(token)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Invalid or expired token",
			})
		}

		// Store user info in context
		userID := strings.TrimSpace(claims.UserID)
		if userID == "" {
			userID = strings.TrimSpace(claims.Subject)
		}
		if userID == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Missing Clerk user id",
			})
		}
		name := firstNonEmpty(claims.FullName, claims.Name, strings.TrimSpace(claims.FirstName+" "+claims.LastName), claims.Username)
		c.Locals("uid", userID)
		c.Locals("email", claims.Email)
		c.Locals("firstName", claims.FirstName)
		c.Locals("lastName", claims.LastName)
		c.Locals("name", name)
		c.Locals("imageUrl", claims.ImageURL)

		return c.Next()
	}
}

func verifyClerkToken(tokenString string) (*models.Claims, error) {
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET not configured")
	}

	jwtSecret = strings.Trim(jwtSecret, "\"'")

	token, err := jwt.ParseWithClaims(tokenString, &models.Claims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(jwtSecret), nil
	})

	if err != nil {
		return nil, fmt.Errorf("token verification failed: %w", err)
	}

	claims, ok := token.Claims.(*models.Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	if claims.ExpiresAt != nil && claims.ExpiresAt.Before(time.Now()) {
		return nil, fmt.Errorf("token expired")
	}

	return claims, nil
}

func RequireAdmin() fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := GetUserID(c)
		if userID == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Missing authenticated user"})
		}
		userOrm := orm.Load(&models.User{})
		defer userOrm.Close()
		var users []models.User
		if err := userOrm.GetByFieldEquals("Id", userID).Scan(&users); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Unable to verify admin access"})
		}
		if len(users) == 0 || !users[0].IsAdmin {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Admin access required"})
		}
		return c.Next()
	}
}

func GetUserID(c *fiber.Ctx) string {
	if userID, ok := c.Locals("uid").(string); ok {
		userID = strings.TrimSpace(userID)
		if userID != "" {
			return userID
		}
	}
	return ""
}

func GetUserEmail(c *fiber.Ctx) string {
	if email, ok := c.Locals("email").(string); ok {
		return email
	}
	return ""
}

func GetUserName(c *fiber.Ctx) string {
	if name, ok := c.Locals("name").(string); ok {
		return name
	}
	return ""
}

func GetUserFirstName(c *fiber.Ctx) string {
	if name, ok := c.Locals("firstName").(string); ok {
		return name
	}
	return ""
}

func GetUserLastName(c *fiber.Ctx) string {
	if name, ok := c.Locals("lastName").(string); ok {
		return name
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
