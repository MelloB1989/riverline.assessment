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
	Name      string         `json:"name,omitempty"`
	ImageURL  string         `json:"image_url,omitempty"`
	Username  string         `json:"username,omitempty"`
	UserID    string         `json:"user_id,omitempty"`
	Metadata  map[string]any `json:"public_metadata,omitempty"`
}

type AppConfig struct {
	Environment              string  `env:"ENV" optional:"true" default:"DEV"`
	AwsAccessKeyId           string  `env:"AWS_ACCESS_KEY_ID"`
	AwsSecretAccessKey       string  `env:"AWS_SECRET_ACCESS_KEY"`
	AwsRegion                string  `env:"AWS_REGION"`
	AwsSesRegion             string  `env:"AWS_SES_REGION"`
	BucketName               string  `env:"BUCKET_NAME"`
	BucketRegion             string  `env:"BUCKET_REGION"`
	MailerAddress            string  `env:"MAILER_ADDRESS"`
	GroqApiKey               string  `env:"GROQ_API_KEY"`
	InternalAppKey           string  `env:"INTERNAL_API_KEY"`
	JwtSecret                string  `env:"JWT_SECRET"`
	DatabaseURL              string  `env:"DATABASE_URL"`
	RedisURL                 string  `env:"REDIS_URL"`
	TemporalHostPort         string  `env:"TEMPORAL_HOST_PORT" optional:"true" default:"localhost:7233"`
	VapiApiKey               string  `env:"VAPI_API_KEY"`
	VapiAssistantId          string  `env:"VAPI_ASSISTANT_ID"`
	VapiPhoneNumberId        string  `env:"VAPI_PHONE_NUMBER_ID"`
	VapiWebhookSecret        string  `env:"VAPI_WEBHOOK_SECRET"`
	VapiDryRun               bool    `env:"VAPI_DRY_RUN" optional:"true" default:"false"`
	PersonaLLMBaseURL        string  `env:"PERSONA_LLM_BASE_URL" optional:"true" default:"https://api.anthropic.com"`
	PersonaLLMApiKey         string  `env:"PERSONA_LLM_API_KEY" optional:"true" default:""`
	PersonaLLMModel          string  `env:"PERSONA_LLM_MODEL" optional:"true" default:"claude-3-5-haiku-20241022"`
	EvaluatorJudges          string  `env:"EVALUATOR_JUDGES_JSON" optional:"true" default:""`
	PromptGenProvider        string  `env:"PROMPT_GENERATOR_PROVIDER" optional:"true" default:"xai"`
	PromptGenModel           string  `env:"PROMPT_GENERATOR_MODEL" optional:"true" default:"grok-4-fast-reasoning"`
	PromptGenMaxTokens       int     `env:"PROMPT_GENERATOR_MAX_TOKENS" optional:"true" default:"1500"`
	PromptGenReasoningEffort string  `env:"PROMPT_GENERATOR_REASONING_EFFORT" optional:"true" default:"none"`
	LearningLoopBudgetUSD    float64 `env:"LEARNING_LOOP_BUDGET_USD" optional:"true" default:"15"`
	NvidiaNIMRPM             int     `env:"NVIDIA_NIM_REQUESTS_PER_MINUTE" optional:"true" default:"35"`
	LlmPricing               string  `env:"LLM_PRICING_JSON" optional:"true" default:""`
	Port                     string  `env:"PORT" optional:"true" default:"8080"`
}
