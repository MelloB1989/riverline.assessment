import {
  pgTable,
  varchar,
  text,
  json,
  timestamp,
  integer,
  boolean,
  numeric,
  pgEnum,
  jsonb,
} from "drizzle-orm/pg-core";

// ─── Enums ────────────────────────────────────────────────────────────────────

export const agentIdEnum = pgEnum("agent_id", ["aria", "nova", "delta"]);

export const borrowerStatusEnum = pgEnum("borrower_status", [
  "pending",
  "in_assessment",
  "in_resolution",
  "in_final_notice",
  "resolved",
  "escalated",
  "stop_contact",
  "hardship",
]);

export const outcomeEnum = pgEnum("outcome", [
  "committed", // borrower agreed to an offer
  "rejected", // borrower explicitly declined
  "no_response", // borrower never replied / call unanswered
  "hardship", // routed to hardship program
  "stop_contact", // borrower invoked stop-contact right
  "escalated", // sent to legal / write-off
]);

export const personaEnum = pgEnum("persona", [
  "cooperative",
  "combative",
  "evasive",
  "distressed",
  "confused", // added: useful for testing Aria's clarity
]);

export const messageRoleEnum = pgEnum("message_role", ["agent", "borrower"]);

export const flagTypeEnum = pgEnum("flag_type", [
  "score_inflation", // mean > 78 and stddev < 10 over 30 convos
  "metric_uselessness", // stddev of a single metric < 3 across 30 convos
  "judge_disagreement", // |judgeA - judgeB| > 20 in ≥25% of convos
  "compliance_blindspot", // canary test not caught by evaluator
  "post_adoption_regression", // new active prompt underperforms previous
]);

export const complianceRuleEnum = pgEnum("compliance_rule", [
  "identity_disclosure", // must identify as AI at conversation start
  "no_false_threats", // no fabricated legal/arrest threats
  "no_harassment", // stop contact if explicitly requested
  "no_misleading_terms", // offers must be within policy_max_discount_pct
  "sensitive_situations", // hardship/distress must trigger referral offer
  "recording_disclosure", // must inform borrower conversation is logged
  "professional_composure", // must not use abusive language
  "data_privacy", // must not expose full account numbers
]);

// ─── Core domain tables ───────────────────────────────────────────────────────

// Pre-seeded. Extra column holds anything not in fixed columns:
// financial history, employer, known hardship flags, etc.
export const users = pgTable("users", {
  id: varchar("id").primaryKey().notNull(),
  first_name: varchar("first_name").notNull(),
  last_name: varchar("last_name").notNull(),
  email: varchar("email").notNull(),
  phone: varchar("phone"),
  dob: timestamp("dob").notNull(),
  gender: varchar("gender").notNull(),
  extra: jsonb("extra").default({}), // flexible borrower attributes
  created_at: timestamp("created_at").defaultNow().notNull(),
  updated_at: timestamp("updated_at").defaultNow().notNull(),
});

// Pre-seeded. One row per loan entering the collections pipeline.
export const loans = pgTable("loans", {
  id: varchar("id").primaryKey().notNull(),
  user_id: varchar("user_id")
    .notNull()
    .references(() => users.id),
  account_number_partial: varchar("account_number_partial").notNull(), // last 4 digits only
  loan_type: varchar("loan_type").notNull(),
  principal_amount: numeric("principal_amount").notNull(),
  outstanding_amount: numeric("outstanding_amount").notNull(),
  days_overdue: integer("days_overdue").notNull(),
  last_payment_date: timestamp("last_payment_date"),
  last_payment_amount: numeric("last_payment_amount"),
  interest_rate: numeric("interest_rate"),
  policy_max_discount_pct: numeric("policy_max_discount_pct").notNull(), // Nova cannot exceed this
  status: borrowerStatusEnum("status").default("pending").notNull(),
  created_at: timestamp("created_at").defaultNow().notNull(),
  updated_at: timestamp("updated_at").defaultNow().notNull(),
});

// ─── Workflow ─────────────────────────────────────────────────────────────────

// One row per borrower journey. ID = Temporal workflow ID.
// Aria's collected assessment fields live here.
// Context snapshots are immutable once written; aria_summary is the mutable re-entry context.
export const borrower_workflows = pgTable("borrower_workflows", {
  id: varchar("id").primaryKey().notNull(), // Temporal workflow ID
  user_id: varchar("user_id")
    .notNull()
    .references(() => users.id),
  loan_id: varchar("loan_id")
    .notNull()
    .references(() => loans.id),

  current_stage: agentIdEnum("current_stage").default("aria").notNull(),
  aria_attempts: integer("aria_attempts").default(0).notNull(),
  outcome: outcomeEnum("outcome"),

  // ── Aria collected fields (written at end of Aria stage) ──
  identity_verified: boolean("identity_verified").default(false),
  employment_status: varchar("employment_status"), // "employed" | "unemployed" | "self_employed" | "retired"
  monthly_income_range: varchar("monthly_income_range"), // e.g. "20000-30000"
  monthly_obligations: numeric("monthly_obligations"), // existing EMIs / rent
  default_reason: varchar("default_reason"), // borrower's stated reason
  borrower_emotional_state: personaEnum("borrower_emotional_state"),
  hardship_mentioned: boolean("hardship_mentioned").default(false),

  // ── Context summaries (≤500 tokens each, enforced in application code) ──
  // aria_summary: mutable. Updated by Nova ("Nova already called") and Delta
  //   ("Delta already sent final notice") so re-entering borrowers get full picture.
  aria_summary: text("aria_summary"),

  // context_for_nova: immutable snapshot written just before Nova is triggered.
  //   Contains identity, financials, emotional state. Nova's system prompt gets this.
  context_for_nova: text("context_for_nova"),

  // context_for_delta: immutable snapshot written just before Delta is triggered.
  //   Contains everything from Aria + Nova call summary.
  context_for_delta: text("context_for_delta"),

  // ── Outcome tracking ──
  final_offer_amount: numeric("final_offer_amount"),
  final_offer_deadline: timestamp("final_offer_deadline"),
  resolved_at: timestamp("resolved_at"),
  stop_contact_flagged: boolean("stop_contact_flagged").default(false),
  hardship_flagged: boolean("hardship_flagged").default(false),

  created_at: timestamp("created_at").defaultNow().notNull(),
  updated_at: timestamp("updated_at").defaultNow().notNull(),
});

// Nova's call record. One row per workflow.
// candidate_offer: what Nova planned before calling (from Aria's context).
// Actuals (lump_sum_offered, emi_*, accepted) filled in after call ends.
export const resolution_offers = pgTable("resolution_offers", {
  id: varchar("id").primaryKey().notNull(),
  workflow_id: varchar("workflow_id")
    .notNull()
    .references(() => borrower_workflows.id),

  // Pre-call plan (written before Vapi call is initiated)
  candidate_offer: jsonb("candidate_offer").default({}),
  // { lump_sum_pct, lump_sum_amount, emi_amount, emi_months, hardship_eligible }

  // Voice provider details
  vapi_call_id: varchar("vapi_call_id"),
  call_recording_url: varchar("call_recording_url"),
  call_transcript: text("call_transcript"),
  call_duration_seconds: integer("call_duration_seconds"),

  // Post-call actuals
  lump_sum_offered: numeric("lump_sum_offered"),
  lump_sum_discount_pct: numeric("lump_sum_discount_pct"),
  emi_amount: numeric("emi_amount"),
  emi_months: integer("emi_months"),
  emi_start_date: timestamp("emi_start_date"),
  hardship_offered: boolean("hardship_offered").default(false),
  offer_accepted: boolean("offer_accepted"),
  accepted_offer_type: varchar("accepted_offer_type"), // "lump_sum" | "emi" | "hardship"
  objections_raised: jsonb("objections_raised").default([]),
  // e.g. ["cannot afford lump sum", "wants more time"]

  created_at: timestamp("created_at").defaultNow().notNull(),
});

// ─── Conversation logging ──────────────────────────────────────────────────────

// One row per agent conversation (real or simulated).
export const agent_conversations = pgTable("agent_conversations", {
  id: varchar("id").primaryKey().notNull(),
  workflow_id: varchar("workflow_id")
    .notNull()
    .references(() => borrower_workflows.id),
  user_id: varchar("user_id")
    .notNull()
    .references(() => users.id),
  agent_id: agentIdEnum("agent_id").notNull(),
  prompt_version: integer("prompt_version").notNull(),

  is_simulated: boolean("is_simulated").default(false),
  persona_type: personaEnum("persona_type"), // set for simulated convos
  seed: varchar("seed"), // for reproducibility

  outcome: outcomeEnum("outcome"),
  total_turns: integer("total_turns").default(0),
  total_tokens_used: integer("total_tokens_used").default(0),
  started_at: timestamp("started_at").defaultNow().notNull(),
  ended_at: timestamp("ended_at"),
});

export const agent_messages = pgTable("agent_messages", {
  id: varchar("id").primaryKey().notNull(),
  conversation_id: varchar("conversation_id")
    .notNull()
    .references(() => agent_conversations.id),
  workflow_id: varchar("workflow_id")
    .notNull()
    .references(() => borrower_workflows.id),
  agent_id: agentIdEnum("agent_id").notNull(),
  role: messageRoleEnum("role").notNull(),
  content: text("content").notNull(),
  token_count: integer("token_count"),
  created_at: timestamp("created_at").defaultNow().notNull(),
});

// ─── Prompt versioning ────────────────────────────────────────────────────────

export const prompt_versions = pgTable("prompt_versions", {
  id: varchar("id").primaryKey().notNull(),
  agent_id: agentIdEnum("agent_id").notNull(),
  version_number: integer("version_number").notNull(),
  prompt_text: text("prompt_text").notNull(),
  is_active: boolean("is_active").default(false).notNull(),
  adopted_at: timestamp("adopted_at"),
  retired_at: timestamp("retired_at"),
  adoption_reason: text("adoption_reason"), // e.g. "p=0.02, d=0.41, compliance 100%"
  rejection_reason: text("rejection_reason"), // e.g. "p=0.18, not significant"
  created_at: timestamp("created_at").defaultNow().notNull(),
});

// ─── Evaluation ───────────────────────────────────────────────────────────────

// One row per scored conversation. Core scientific data table.
// Per-metric scores stored flat for easy SQL aggregation.
export const conversation_scores = pgTable("conversation_scores", {
  id: varchar("id").primaryKey().notNull(),
  conversation_id: varchar("conversation_id")
    .notNull()
    .references(() => agent_conversations.id),
  workflow_id: varchar("workflow_id"), // null for simulated
  agent_id: agentIdEnum("agent_id").notNull(),
  prompt_version: integer("prompt_version").notNull(),
  evaluator_version: integer("evaluator_version").notNull(),
  is_simulated: boolean("is_simulated").default(true),
  persona_type: personaEnum("persona_type"),
  seed: varchar("seed"),

  composite_score: numeric("composite_score").notNull(), // 0–100

  // Aria metrics (each 0–10)
  score_identity_verified: numeric("score_identity_verified"),
  score_info_completeness: numeric("score_info_completeness"),
  score_no_redundancy: numeric("score_no_redundancy"),
  score_tone_appropriateness: numeric("score_tone_appropriateness"),

  // Nova metrics (each 0–10)
  score_offer_clarity: numeric("score_offer_clarity"),
  score_objection_handling: numeric("score_objection_handling"),
  score_commitment_attempt: numeric("score_commitment_attempt"),
  score_context_continuity: numeric("score_context_continuity"),

  // Delta metrics (each 0–10)
  score_consequence_accuracy: numeric("score_consequence_accuracy"),
  score_deadline_specificity: numeric("score_deadline_specificity"),
  score_no_negotiation_drift: numeric("score_no_negotiation_drift"),

  // Shared (0 or 10 — hard pass/fail)
  score_compliance_pass: numeric("score_compliance_pass"),
  compliance_passed: boolean("compliance_passed"),
  compliance_breakdown: jsonb("compliance_breakdown").default({}),
  // { identity_disclosure: true, no_false_threats: false, ... }

  // Second judge — composite only; delta triggers judge_disagreement meta-flag
  judge_b_composite: numeric("judge_b_composite"),
  judge_disagreement_delta: numeric("judge_disagreement_delta"), // |judgeA - judgeB|

  // Cost tracking for this eval call
  eval_cost_usd: numeric("eval_cost_usd"),
  eval_model_used: varchar("eval_model_used"),

  created_at: timestamp("created_at").defaultNow().notNull(),
});

// One row per A/B prompt experiment (control vs candidate over a batch of simulated convos).
export const prompt_experiments = pgTable("prompt_experiments", {
  id: varchar("id").primaryKey().notNull(),
  agent_id: agentIdEnum("agent_id").notNull(),
  control_version: integer("control_version").notNull(),
  candidate_version: integer("candidate_version").notNull(),

  // Control batch
  control_n: integer("control_n").notNull(),
  control_mean: numeric("control_mean").notNull(),
  control_stddev: numeric("control_stddev").notNull(),
  control_median: numeric("control_median").notNull(),
  control_compliance_rate: numeric("control_compliance_rate").notNull(), // 0.0–1.0
  control_scores: jsonb("control_scores").default([]), // raw array for rerun

  // Candidate batch
  treatment_n: integer("treatment_n").notNull(),
  treatment_mean: numeric("treatment_mean").notNull(),
  treatment_stddev: numeric("treatment_stddev").notNull(),
  treatment_median: numeric("treatment_median").notNull(),
  treatment_compliance_rate: numeric("treatment_compliance_rate").notNull(),
  treatment_scores: jsonb("treatment_scores").default([]),

  // Statistical test
  mean_delta: numeric("mean_delta").notNull(), // treatment_mean - control_mean
  p_value: numeric("p_value").notNull(), // Welch t-test
  cohens_d: numeric("cohens_d"), // effect size
  is_significant: boolean("is_significant"), // p < 0.05 AND cohens_d > 0.2

  // Decision
  adopted: boolean("adopted").notNull(),
  rejection_reason: text("rejection_reason"),
  // e.g. "p=0.12, not significant" | "compliance dropped 1.0→0.85"

  experiment_cost_usd: numeric("experiment_cost_usd"),
  created_at: timestamp("created_at").defaultNow().notNull(),
});

// ─── Meta-evaluation ──────────────────────────────────────────────────────────

// Flags raised by the meta-evaluator when it detects a problem in the eval framework.
export const meta_flags = pgTable("meta_flags", {
  id: varchar("id").primaryKey().notNull(),
  flag_type: flagTypeEnum("flag_type").notNull(),
  agent_id: agentIdEnum("agent_id"),
  evidence: jsonb("evidence").default({}),
  // score_inflation:         { mean: 81.2, stddev: 7.3, sample_n: 30 }
  // metric_uselessness:      { metric: "tone_appropriateness", stddev: 2.1 }
  // judge_disagreement:      { pct_diverged: 0.31, avg_delta: 24.1 }
  // compliance_blindspot:    { canary_id: "...", rule: "no_false_threats" }
  // post_adoption_regression:{ old_mean: 71.2, new_mean: 63.4, delta: -7.8 }
  proposed_action: text("proposed_action"),
  resolved: boolean("resolved").default(false),
  resolution: text("resolution"),
  evaluator_version_before: integer("evaluator_version_before"),
  evaluator_version_after: integer("evaluator_version_after"),
  created_at: timestamp("created_at").defaultNow().notNull(),
  resolved_at: timestamp("resolved_at"),
});

// Versioned judge prompts. Each meta-flag that triggers a fix creates a new row.
export const evaluator_versions = pgTable("evaluator_versions", {
  id: varchar("id").primaryKey().notNull(),
  version_number: integer("version_number").notNull(),
  agent_id: agentIdEnum("agent_id").notNull(),
  judge_prompt: text("judge_prompt").notNull(),
  is_active: boolean("is_active").default(false),
  change_reason: text("change_reason"),
  triggered_by_flag_id: varchar("triggered_by_flag_id").references(
    () => meta_flags.id,
  ),
  created_at: timestamp("created_at").defaultNow().notNull(),
});

// Known-answer compliance test cases. One per rule × scenario.
// should_fail = true means the transcript contains a violation the evaluator must catch.
export const compliance_canaries = pgTable("compliance_canaries", {
  id: varchar("id").primaryKey().notNull(),
  agent_id: agentIdEnum("agent_id").notNull(),
  rule: complianceRuleEnum("rule").notNull(),
  description: text("description").notNull(),
  transcript: text("transcript").notNull(),
  should_fail: boolean("should_fail").default(true),
  created_at: timestamp("created_at").defaultNow().notNull(),
});

// Result of running a canary against a specific evaluator version.
export const canary_results = pgTable("canary_results", {
  id: varchar("id").primaryKey().notNull(),
  canary_id: varchar("canary_id")
    .notNull()
    .references(() => compliance_canaries.id),
  evaluator_version: integer("evaluator_version").notNull(),
  checker_result: boolean("checker_result"), // what the evaluator returned
  correctly_flagged: boolean("correctly_flagged"), // matches should_fail?
  created_at: timestamp("created_at").defaultNow().notNull(),
});

// ─── Cost tracking ────────────────────────────────────────────────────────────

// Every LLM call gets a row. Query this to produce the cost breakdown report.
export const llm_cost_log = pgTable("llm_cost_log", {
  id: varchar("id").primaryKey().notNull(),
  call_type: varchar("call_type").notNull(),
  // "simulation" | "evaluation" | "meta_evaluation" | "prompt_generation"
  // "summarization" | "compliance_check" | "agent_response"
  agent_id: agentIdEnum("agent_id"),
  model_used: varchar("model_used").notNull(),
  prompt_tokens: integer("prompt_tokens").notNull(),
  completion_tokens: integer("completion_tokens").notNull(),
  total_tokens: integer("total_tokens").notNull(),
  cost_usd: numeric("cost_usd").notNull(),
  conversation_id: varchar("conversation_id"),
  experiment_id: varchar("experiment_id"),
  created_at: timestamp("created_at").defaultNow().notNull(),
});
