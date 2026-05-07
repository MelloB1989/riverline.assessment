ALTER TYPE "persona" ADD VALUE 'confused';--> statement-breakpoint
DROP TABLE "assessments";--> statement-breakpoint
DROP TABLE "eval_runs";--> statement-breakpoint
DROP TABLE "user_memories";--> statement-breakpoint
ALTER TABLE "borrower_workflows" RENAME COLUMN "nova_summary" TO "identity_verified";--> statement-breakpoint
ALTER TABLE "agent_messages" DROP CONSTRAINT "agent_messages_user_id_users_id_fk";
--> statement-breakpoint
ALTER TABLE "borrower_workflows" ALTER COLUMN "identity_verified" SET DATA TYPE boolean;--> statement-breakpoint
ALTER TABLE "borrower_workflows" ALTER COLUMN "identity_verified" SET DEFAULT false;--> statement-breakpoint
ALTER TABLE "prompt_experiments" ALTER COLUMN "control_compliance_rate" SET NOT NULL;--> statement-breakpoint
ALTER TABLE "prompt_experiments" ALTER COLUMN "treatment_compliance_rate" SET NOT NULL;--> statement-breakpoint
ALTER TABLE "resolution_offers" ALTER COLUMN "objections_raised" SET DATA TYPE jsonb;--> statement-breakpoint
ALTER TABLE "resolution_offers" ALTER COLUMN "objections_raised" SET DEFAULT '[]'::jsonb;--> statement-breakpoint
ALTER TABLE "users" ALTER COLUMN "extra" SET DATA TYPE jsonb;--> statement-breakpoint
ALTER TABLE "users" ALTER COLUMN "extra" SET DEFAULT '{}'::jsonb;--> statement-breakpoint
ALTER TABLE "borrower_workflows" ADD COLUMN "employment_status" varchar;--> statement-breakpoint
ALTER TABLE "borrower_workflows" ADD COLUMN "monthly_income_range" varchar;--> statement-breakpoint
ALTER TABLE "borrower_workflows" ADD COLUMN "monthly_obligations" numeric;--> statement-breakpoint
ALTER TABLE "borrower_workflows" ADD COLUMN "default_reason" varchar;--> statement-breakpoint
ALTER TABLE "borrower_workflows" ADD COLUMN "borrower_emotional_state" "persona";--> statement-breakpoint
ALTER TABLE "borrower_workflows" ADD COLUMN "hardship_mentioned" boolean DEFAULT false;--> statement-breakpoint
ALTER TABLE "borrower_workflows" ADD COLUMN "context_for_nova" text;--> statement-breakpoint
ALTER TABLE "borrower_workflows" ADD COLUMN "context_for_delta" text;--> statement-breakpoint
ALTER TABLE "resolution_offers" ADD COLUMN "candidate_offer" jsonb DEFAULT '{}'::jsonb;--> statement-breakpoint
ALTER TABLE "agent_conversations" DROP COLUMN IF EXISTS "summary";--> statement-breakpoint
ALTER TABLE "agent_messages" DROP COLUMN IF EXISTS "user_id";--> statement-breakpoint
ALTER TABLE "agent_messages" DROP COLUMN IF EXISTS "tool_calls";--> statement-breakpoint
ALTER TABLE "agent_messages" DROP COLUMN IF EXISTS "tool_call_id";--> statement-breakpoint
ALTER TABLE "agent_messages" DROP COLUMN IF EXISTS "images";--> statement-breakpoint
ALTER TABLE "agent_messages" DROP COLUMN IF EXISTS "files";--> statement-breakpoint
ALTER TABLE "conversation_scores" DROP COLUMN IF EXISTS "judge_b_metric_scores";--> statement-breakpoint
ALTER TABLE "prompt_experiments" DROP COLUMN IF EXISTS "control_p10";--> statement-breakpoint
ALTER TABLE "prompt_experiments" DROP COLUMN IF EXISTS "control_p90";--> statement-breakpoint
ALTER TABLE "prompt_experiments" DROP COLUMN IF EXISTS "control_min";--> statement-breakpoint
ALTER TABLE "prompt_experiments" DROP COLUMN IF EXISTS "control_max";--> statement-breakpoint
ALTER TABLE "prompt_experiments" DROP COLUMN IF EXISTS "treatment_p10";--> statement-breakpoint
ALTER TABLE "prompt_experiments" DROP COLUMN IF EXISTS "treatment_p90";--> statement-breakpoint
ALTER TABLE "prompt_experiments" DROP COLUMN IF EXISTS "treatment_min";--> statement-breakpoint
ALTER TABLE "prompt_experiments" DROP COLUMN IF EXISTS "treatment_max";--> statement-breakpoint
ALTER TABLE "users" DROP COLUMN IF EXISTS "pfp";--> statement-breakpoint
ALTER TABLE "users" DROP COLUMN IF EXISTS "bio";--> statement-breakpoint
ALTER TABLE "users" DROP COLUMN IF EXISTS "billing_address";