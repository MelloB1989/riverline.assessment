package models

import (
	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	jwt.RegisteredClaims
	Email     string         `json:"email,omitempty"`
	FirstName string         `json:"first_name,omitempty"`
	LastName  string         `json:"last_name,omitempty"`
	FullName  string         `json:"full_name,omitempty"`
	ImageURL  string         `json:"image_url,omitempty"`
	Username  string         `json:"username,omitempty"`
	UserID    string         `json:"user_id,omitempty"`
	Metadata  map[string]any `json:"public_metadata,omitempty"`
}

type AppConfig struct {
	AwsAccessKeyId     string `env:"AWS_ACCESS_KEY_ID"`
	AwsSecretAccessKey string `env:"AWS_SECRET_ACCESS_KEY"`
	AwsRegion          string `env:"AWS_REGION"`
	AwsSesRegion       string `env:"AWS_SES_REGION"`
	BucketName         string `env:"BUCKET_NAME"`
	BucketRegion       string `env:"BUCKET_REGION"`
	GroqApiKey         string `env:"GROQ_API_KEY"`
	InternalAppKey     string `env:"INTERNAL_API_KEY"`
	JwtSecret          string `env:"JWT_SECRET"`
	DatabaseURL        string `env:"DATABASE_URL"`
	RedisURL           string `env:"REDIS_URL"`
	TemporalHostPort   string `env:"TEMPORAL_HOST_PORT" optional:"true" default:"temporal:7233"`
	VapiApiKey         string `env:"VAPI_API_KEY" optional:"true"`
	VapiAssistantId    string `env:"VAPI_ASSISTANT_ID" optional:"true"`
	VapiPhoneNumberId  string `env:"VAPI_PHONE_NUMBER_ID" optional:"true"`
	VapiWebhookSecret  string `env:"VAPI_WEBHOOK_SECRET" optional:"true"`
	Port               string `env:"PORT" optional:"true" default:"8080"`
}
