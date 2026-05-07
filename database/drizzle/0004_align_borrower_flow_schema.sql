ALTER TYPE "persona" ADD VALUE IF NOT EXISTS 'confused';
--> statement-breakpoint
ALTER TABLE "users" DROP COLUMN IF EXISTS "pfp";
--> statement-breakpoint
ALTER TABLE "users" DROP COLUMN IF EXISTS "bio";
--> statement-breakpoint
ALTER TABLE "users" DROP COLUMN IF EXISTS "billing_address";
--> statement-breakpoint
ALTER TABLE "users" ALTER COLUMN "extra" TYPE jsonb USING "extra"::jsonb;
--> statement-breakpoint
ALTER TABLE "borrower_workflows" ADD COLUMN IF NOT EXISTS "identity_verified" boolean DEFAULT false;
--> statement-breakpoint
ALTER TABLE "borrower_workflows" ADD COLUMN IF NOT EXISTS "employment_status" varchar;
--> statement-breakpoint
ALTER TABLE "borrower_workflows" ADD COLUMN IF NOT EXISTS "monthly_income_range" varchar;
--> statement-breakpoint
ALTER TABLE "borrower_workflows" ADD COLUMN IF NOT EXISTS "monthly_obligations" numeric;
--> statement-breakpoint
ALTER TABLE "borrower_workflows" ADD COLUMN IF NOT EXISTS "default_reason" varchar;
--> statement-breakpoint
ALTER TABLE "borrower_workflows" ADD COLUMN IF NOT EXISTS "borrower_emotional_state" "persona";
--> statement-breakpoint
ALTER TABLE "borrower_workflows" ADD COLUMN IF NOT EXISTS "hardship_mentioned" boolean DEFAULT false;
--> statement-breakpoint
ALTER TABLE "borrower_workflows" ADD COLUMN IF NOT EXISTS "context_for_nova" text;
--> statement-breakpoint
ALTER TABLE "borrower_workflows" ADD COLUMN IF NOT EXISTS "context_for_delta" text;
--> statement-breakpoint
ALTER TABLE "borrower_workflows" DROP COLUMN IF EXISTS "nova_summary";
--> statement-breakpoint
ALTER TABLE "resolution_offers" ADD COLUMN IF NOT EXISTS "candidate_offer" jsonb DEFAULT '{}'::jsonb;
--> statement-breakpoint
ALTER TABLE "agent_conversations" DROP COLUMN IF EXISTS "summary";
--> statement-breakpoint
ALTER TABLE "agent_messages" DROP COLUMN IF EXISTS "user_id";
--> statement-breakpoint
ALTER TABLE "conversation_scores" DROP COLUMN IF EXISTS "judge_b_metric_scores";
--> statement-breakpoint
ALTER TABLE "prompt_experiments" DROP COLUMN IF EXISTS "control_p10";
--> statement-breakpoint
ALTER TABLE "prompt_experiments" DROP COLUMN IF EXISTS "control_p90";
--> statement-breakpoint
ALTER TABLE "prompt_experiments" DROP COLUMN IF EXISTS "control_min";
--> statement-breakpoint
ALTER TABLE "prompt_experiments" DROP COLUMN IF EXISTS "control_max";
--> statement-breakpoint
ALTER TABLE "prompt_experiments" DROP COLUMN IF EXISTS "treatment_p10";
--> statement-breakpoint
ALTER TABLE "prompt_experiments" DROP COLUMN IF EXISTS "treatment_p90";
--> statement-breakpoint
ALTER TABLE "prompt_experiments" DROP COLUMN IF EXISTS "treatment_min";
--> statement-breakpoint
ALTER TABLE "prompt_experiments" DROP COLUMN IF EXISTS "treatment_max";
--> statement-breakpoint
UPDATE "prompt_experiments" SET "control_compliance_rate" = 0 WHERE "control_compliance_rate" IS NULL;
--> statement-breakpoint
UPDATE "prompt_experiments" SET "treatment_compliance_rate" = 0 WHERE "treatment_compliance_rate" IS NULL;
--> statement-breakpoint
ALTER TABLE "prompt_experiments" ALTER COLUMN "control_compliance_rate" SET NOT NULL;
--> statement-breakpoint
ALTER TABLE "prompt_experiments" ALTER COLUMN "treatment_compliance_rate" SET NOT NULL;
--> statement-breakpoint
DROP TABLE IF EXISTS "assessments";
--> statement-breakpoint
DROP TABLE IF EXISTS "user_memories";
--> statement-breakpoint
DROP TABLE IF EXISTS "eval_runs";
