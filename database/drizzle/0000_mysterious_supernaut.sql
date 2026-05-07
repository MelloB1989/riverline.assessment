DO $$ BEGIN
 CREATE TYPE "public"."agent_id" AS ENUM('aria', 'nova', 'delta');
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
DO $$ BEGIN
 CREATE TYPE "public"."borrower_status" AS ENUM('pending', 'in_assessment', 'in_resolution', 'in_final_notice', 'resolved', 'escalated', 'stop_contact', 'hardship');
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
DO $$ BEGIN
 CREATE TYPE "public"."compliance_rule" AS ENUM('identity_disclosure', 'no_false_threats', 'no_harassment', 'no_misleading_terms', 'sensitive_situations', 'recording_disclosure', 'professional_composure', 'data_privacy');
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
DO $$ BEGIN
 CREATE TYPE "public"."flag_type" AS ENUM('score_inflation', 'metric_uselessness', 'judge_disagreement', 'compliance_blindspot', 'post_adoption_regression');
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
DO $$ BEGIN
 CREATE TYPE "public"."message_role" AS ENUM('agent', 'borrower');
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
DO $$ BEGIN
 CREATE TYPE "public"."outcome" AS ENUM('committed', 'rejected', 'no_response', 'hardship', 'stop_contact', 'escalated');
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
DO $$ BEGIN
 CREATE TYPE "public"."persona" AS ENUM('cooperative', 'combative', 'evasive', 'distressed', 'confused');
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS "agent_conversations" (
	"id" varchar PRIMARY KEY NOT NULL,
	"workflow_id" varchar NOT NULL,
	"user_id" varchar NOT NULL,
	"agent_id" "agent_id" NOT NULL,
	"prompt_version" integer NOT NULL,
	"is_simulated" boolean DEFAULT false,
	"persona_type" "persona",
	"seed" varchar,
	"outcome" "outcome",
	"total_turns" integer DEFAULT 0,
	"total_tokens_used" integer DEFAULT 0,
	"started_at" timestamp DEFAULT now() NOT NULL,
	"ended_at" timestamp
);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS "agent_messages" (
	"id" varchar PRIMARY KEY NOT NULL,
	"conversation_id" varchar NOT NULL,
	"workflow_id" varchar NOT NULL,
	"agent_id" "agent_id" NOT NULL,
	"role" "message_role" NOT NULL,
	"content" text NOT NULL,
	"token_count" integer,
	"created_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS "borrower_workflows" (
	"id" varchar PRIMARY KEY NOT NULL,
	"user_id" varchar NOT NULL,
	"loan_id" varchar NOT NULL,
	"current_stage" "agent_id" DEFAULT 'aria' NOT NULL,
	"aria_attempts" integer DEFAULT 0 NOT NULL,
	"outcome" "outcome",
	"identity_verified" boolean DEFAULT false,
	"employment_status" varchar,
	"monthly_income_range" varchar,
	"monthly_obligations" real,
	"default_reason" varchar,
	"borrower_emotional_state" "persona",
	"hardship_mentioned" boolean DEFAULT false,
	"aria_summary" text,
	"context_for_nova" text,
	"context_for_delta" text,
	"final_offer_amount" real,
	"final_offer_deadline" timestamp,
	"resolved_at" timestamp,
	"stop_contact_flagged" boolean DEFAULT false,
	"hardship_flagged" boolean DEFAULT false,
	"created_at" timestamp DEFAULT now() NOT NULL,
	"updated_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS "canary_results" (
	"id" varchar PRIMARY KEY NOT NULL,
	"canary_id" varchar NOT NULL,
	"evaluator_version" integer NOT NULL,
	"checker_result" boolean,
	"correctly_flagged" boolean,
	"created_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS "compliance_canaries" (
	"id" varchar PRIMARY KEY NOT NULL,
	"agent_id" "agent_id" NOT NULL,
	"rule" "compliance_rule" NOT NULL,
	"description" text NOT NULL,
	"transcript" text NOT NULL,
	"should_fail" boolean DEFAULT true,
	"created_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS "conversation_scores" (
	"id" varchar PRIMARY KEY NOT NULL,
	"conversation_id" varchar NOT NULL,
	"workflow_id" varchar,
	"agent_id" "agent_id" NOT NULL,
	"prompt_version" integer NOT NULL,
	"evaluator_version" integer NOT NULL,
	"is_simulated" boolean DEFAULT true,
	"persona_type" "persona",
	"seed" varchar,
	"composite_score" real NOT NULL,
	"score_identity_verified" real,
	"score_info_completeness" real,
	"score_no_redundancy" real,
	"score_tone_appropriateness" real,
	"score_offer_clarity" real,
	"score_objection_handling" real,
	"score_commitment_attempt" real,
	"score_context_continuity" real,
	"score_consequence_accuracy" real,
	"score_deadline_specificity" real,
	"score_no_negotiation_drift" real,
	"score_compliance_pass" real,
	"compliance_passed" boolean,
	"compliance_breakdown" jsonb DEFAULT '{}'::jsonb,
	"judge_b_composite" real,
	"judge_disagreement_delta" real,
	"eval_cost_usd" real,
	"eval_model_used" varchar,
	"created_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS "evaluator_versions" (
	"id" varchar PRIMARY KEY NOT NULL,
	"version_number" integer NOT NULL,
	"agent_id" "agent_id" NOT NULL,
	"judge_prompt" text NOT NULL,
	"is_active" boolean DEFAULT false,
	"change_reason" text,
	"triggered_by_flag_id" varchar,
	"created_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS "llm_cost_log" (
	"id" varchar PRIMARY KEY NOT NULL,
	"call_type" varchar NOT NULL,
	"agent_id" "agent_id",
	"model_used" varchar NOT NULL,
	"prompt_tokens" integer NOT NULL,
	"completion_tokens" integer NOT NULL,
	"total_tokens" integer NOT NULL,
	"cost_usd" real NOT NULL,
	"conversation_id" varchar,
	"experiment_id" varchar,
	"created_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS "loans" (
	"id" varchar PRIMARY KEY NOT NULL,
	"user_id" varchar NOT NULL,
	"account_number_partial" varchar NOT NULL,
	"loan_type" varchar NOT NULL,
	"principal_amount" real NOT NULL,
	"outstanding_amount" real NOT NULL,
	"days_overdue" integer NOT NULL,
	"last_payment_date" timestamp,
	"last_payment_amount" real,
	"interest_rate" real,
	"policy_max_discount_pct" real NOT NULL,
	"status" "borrower_status" DEFAULT 'pending' NOT NULL,
	"created_at" timestamp DEFAULT now() NOT NULL,
	"updated_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS "meta_flags" (
	"id" varchar PRIMARY KEY NOT NULL,
	"flag_type" "flag_type" NOT NULL,
	"agent_id" "agent_id",
	"evidence" jsonb DEFAULT '{}'::jsonb,
	"proposed_action" text,
	"resolved" boolean DEFAULT false,
	"resolution" text,
	"evaluator_version_before" integer,
	"evaluator_version_after" integer,
	"created_at" timestamp DEFAULT now() NOT NULL,
	"resolved_at" timestamp
);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS "prompt_experiments" (
	"id" varchar PRIMARY KEY NOT NULL,
	"agent_id" "agent_id" NOT NULL,
	"control_version" integer NOT NULL,
	"candidate_version" integer NOT NULL,
	"control_n" integer NOT NULL,
	"control_mean" real NOT NULL,
	"control_stddev" real NOT NULL,
	"control_median" real NOT NULL,
	"control_compliance_rate" real NOT NULL,
	"control_scores" jsonb DEFAULT '[]'::jsonb,
	"treatment_n" integer NOT NULL,
	"treatment_mean" real NOT NULL,
	"treatment_stddev" real NOT NULL,
	"treatment_median" real NOT NULL,
	"treatment_compliance_rate" real NOT NULL,
	"treatment_scores" jsonb DEFAULT '[]'::jsonb,
	"mean_delta" real NOT NULL,
	"p_value" real NOT NULL,
	"cohens_d" real,
	"is_significant" boolean,
	"adopted" boolean NOT NULL,
	"rejection_reason" text,
	"experiment_cost_usd" real,
	"created_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS "prompt_versions" (
	"id" varchar PRIMARY KEY NOT NULL,
	"agent_id" "agent_id" NOT NULL,
	"version_number" integer NOT NULL,
	"prompt_text" text NOT NULL,
	"is_active" boolean DEFAULT false NOT NULL,
	"adopted_at" timestamp,
	"retired_at" timestamp,
	"adoption_reason" text,
	"rejection_reason" text,
	"created_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS "resolution_offers" (
	"id" varchar PRIMARY KEY NOT NULL,
	"workflow_id" varchar NOT NULL,
	"candidate_offer" jsonb DEFAULT '{}'::jsonb,
	"vapi_call_id" varchar,
	"call_recording_url" varchar,
	"call_transcript" text,
	"call_duration_seconds" integer,
	"scheduled_call_at" timestamp,
	"lump_sum_offered" real,
	"lump_sum_discount_pct" real,
	"emi_amount" real,
	"emi_months" integer,
	"emi_start_date" timestamp,
	"hardship_offered" boolean DEFAULT false,
	"offer_accepted" boolean,
	"accepted_offer_type" varchar,
	"objections_raised" jsonb DEFAULT '[]'::jsonb,
	"created_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS "users" (
	"id" varchar PRIMARY KEY NOT NULL,
	"first_name" varchar NOT NULL,
	"last_name" varchar NOT NULL,
	"email" varchar NOT NULL,
	"phone" varchar,
	"dob" timestamp NOT NULL,
	"gender" varchar NOT NULL,
	"extra" jsonb DEFAULT '{}'::jsonb,
	"created_at" timestamp DEFAULT now() NOT NULL,
	"updated_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
DO $$ BEGIN
 ALTER TABLE "agent_conversations" ADD CONSTRAINT "agent_conversations_workflow_id_borrower_workflows_id_fk" FOREIGN KEY ("workflow_id") REFERENCES "public"."borrower_workflows"("id") ON DELETE no action ON UPDATE no action;
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
DO $$ BEGIN
 ALTER TABLE "agent_conversations" ADD CONSTRAINT "agent_conversations_user_id_users_id_fk" FOREIGN KEY ("user_id") REFERENCES "public"."users"("id") ON DELETE no action ON UPDATE no action;
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
DO $$ BEGIN
 ALTER TABLE "agent_messages" ADD CONSTRAINT "agent_messages_conversation_id_agent_conversations_id_fk" FOREIGN KEY ("conversation_id") REFERENCES "public"."agent_conversations"("id") ON DELETE no action ON UPDATE no action;
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
DO $$ BEGIN
 ALTER TABLE "agent_messages" ADD CONSTRAINT "agent_messages_workflow_id_borrower_workflows_id_fk" FOREIGN KEY ("workflow_id") REFERENCES "public"."borrower_workflows"("id") ON DELETE no action ON UPDATE no action;
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
DO $$ BEGIN
 ALTER TABLE "borrower_workflows" ADD CONSTRAINT "borrower_workflows_user_id_users_id_fk" FOREIGN KEY ("user_id") REFERENCES "public"."users"("id") ON DELETE no action ON UPDATE no action;
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
DO $$ BEGIN
 ALTER TABLE "borrower_workflows" ADD CONSTRAINT "borrower_workflows_loan_id_loans_id_fk" FOREIGN KEY ("loan_id") REFERENCES "public"."loans"("id") ON DELETE no action ON UPDATE no action;
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
DO $$ BEGIN
 ALTER TABLE "canary_results" ADD CONSTRAINT "canary_results_canary_id_compliance_canaries_id_fk" FOREIGN KEY ("canary_id") REFERENCES "public"."compliance_canaries"("id") ON DELETE no action ON UPDATE no action;
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
DO $$ BEGIN
 ALTER TABLE "conversation_scores" ADD CONSTRAINT "conversation_scores_conversation_id_agent_conversations_id_fk" FOREIGN KEY ("conversation_id") REFERENCES "public"."agent_conversations"("id") ON DELETE no action ON UPDATE no action;
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
DO $$ BEGIN
 ALTER TABLE "evaluator_versions" ADD CONSTRAINT "evaluator_versions_triggered_by_flag_id_meta_flags_id_fk" FOREIGN KEY ("triggered_by_flag_id") REFERENCES "public"."meta_flags"("id") ON DELETE no action ON UPDATE no action;
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
DO $$ BEGIN
 ALTER TABLE "loans" ADD CONSTRAINT "loans_user_id_users_id_fk" FOREIGN KEY ("user_id") REFERENCES "public"."users"("id") ON DELETE no action ON UPDATE no action;
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
DO $$ BEGIN
 ALTER TABLE "resolution_offers" ADD CONSTRAINT "resolution_offers_workflow_id_borrower_workflows_id_fk" FOREIGN KEY ("workflow_id") REFERENCES "public"."borrower_workflows"("id") ON DELETE no action ON UPDATE no action;
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
CREATE UNIQUE INDEX IF NOT EXISTS "borrower_workflows_one_active_per_user_idx" ON "borrower_workflows" USING btree ("user_id") WHERE "borrower_workflows"."outcome" IS NULL AND "borrower_workflows"."resolved_at" IS NULL;