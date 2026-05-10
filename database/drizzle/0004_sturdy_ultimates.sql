ALTER TYPE "outcome" ADD VALUE 'need_hardship_referral';--> statement-breakpoint
ALTER TABLE "prompt_experiments" DROP COLUMN IF EXISTS "agent_id";