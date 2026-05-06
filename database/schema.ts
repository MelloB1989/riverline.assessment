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
  "committed",
  "rejected",
  "no_response",
  "hardship",
  "stop_contact",
  "escalated",
]);

export const personaEnum = pgEnum("persona", [
  "cooperative",
  "combative",
  "evasive",
  "distressed",
]);

export const messageRoleEnum = pgEnum("message_role", ["agent", "borrower"]);

export const flagTypeEnum = pgEnum("flag_type", [
  "score_inflation", // mean > 78 and stddev < 10
  "metric_uselessness", // stddev of a metric < 3 across 30 convos
  "judge_disagreement", // judgeA vs judgeB diff > 20 in 25%+ convos
  "compliance_blindspot", // canary test missed
  "post_adoption_regression", // new prompt underperforms old
]);

export const complianceRuleEnum = pgEnum("compliance_rule", [
  "identity_disclosure", // rule 1: must identify as AI
  "no_false_threats", // rule 2: no fabricated legal threats
  "no_harassment", // rule 3: stop contact if requested
  "no_misleading_terms", // rule 4: offers within policy range
  "sensitive_situations", // rule 5: hardship must trigger referral
  "recording_disclosure", // rule 6: must mention logging/recording
  "professional_composure", // rule 7: no abusive language
  "data_privacy", // rule 8: no full account numbers
]);

export const users = pgTable("users", {
  id: varchar("id").primaryKey().notNull(),
  first_name: varchar("first_name").notNull(),
  last_name: varchar("last_name").notNull(),
  email: varchar("email").notNull(),
  phone: varchar("phone"),
  dob: timestamp("dob").notNull(),
  gender: varchar("gender").notNull(),
  pfp: varchar("pfp").default(""),
  bio: text("bio").notNull(),
  extra: json("extra").default({}),
  created_at: timestamp("created_at").defaultNow().notNull(),
  updated_at: timestamp("updated_at").defaultNow().notNull(),
  billing_address: json("billing_address").default({
    country: "IN",
    state: "",
    city: "",
    street: "",
    zipcode: "",
  }),
});

export const loans = pgTable("loans", {
  id: varchar("id").primaryKey().notNull(),
  user_id: varchar("user_id")
    .notNull()
    .references(() => users.id),
  account_number_partial: varchar("account_number_partial").notNull(),
  loan_type: varchar("loan_type").notNull(),
  principal_amount: numeric("principal_amount").notNull(),
  outstanding_amount: numeric("outstanding_amount").notNull(),
  days_overdue: integer("days_overdue").notNull(),
  last_payment_date: timestamp("last_payment_date"),
  last_payment_amount: numeric("last_payment_amount"),
  interest_rate: numeric("interest_rate"),
  policy_max_discount_pct: numeric("policy_max_discount_pct").notNull(),
  status: borrowerStatusEnum("status").default("pending").notNull(),
  created_at: timestamp("created_at").defaultNow().notNull(),
  updated_at: timestamp("updated_at").defaultNow().notNull(),
});

export const borrower_workflows = pgTable("borrower_workflows", {
  id: varchar("id").primaryKey().notNull(), // = Temporal workflow ID
  user_id: varchar("user_id")
    .notNull()
    .references(() => users.id),
  loan_id: varchar("loan_id")
    .notNull()
    .references(() => loans.id),
  current_stage: agentIdEnum("current_stage").default("aria").notNull(),
  aria_attempts: integer("aria_attempts").default(0).notNull(),
  outcome: outcomeEnum("outcome"),
  aria_summary: text("aria_summary"), // ≤500 tokens enforced in Go
  nova_summary: text("nova_summary"), // ≤500 tokens enforced in Go
  final_offer_amount: numeric("final_offer_amount"),
  final_offer_deadline: timestamp("final_offer_deadline"),
  resolved_at: timestamp("resolved_at"),
  stop_contact_flagged: boolean("stop_contact_flagged").default(false),
  hardship_flagged: boolean("hardship_flagged").default(false),
  created_at: timestamp("created_at").defaultNow().notNull(),
  updated_at: timestamp("updated_at").defaultNow().notNull(),
});

export const assessments = pgTable("assessments", {
  id: varchar("id").primaryKey().notNull(),
  workflow_id: varchar("workflow_id")
    .notNull()
    .references(() => borrower_workflows.id),
  user_id: varchar("user_id")
    .notNull()
    .references(() => users.id),
  identity_verified: boolean("identity_verified").default(false),
  employment_status: varchar("employment_status"),
  monthly_income_range: varchar("monthly_income_range"),
  monthly_obligations: numeric("monthly_obligations"),
  default_reason: varchar("default_reason"),
  borrower_emotional_state: personaEnum("borrower_emotional_state"),
  has_savings: boolean("has_savings"),
  hardship_mentioned: boolean("hardship_mentioned").default(false),
  created_at: timestamp("created_at").defaultNow().notNull(),
});

export const resolution_offers = pgTable("resolution_offers", {
  id: varchar("id").primaryKey().notNull(),
  workflow_id: varchar("workflow_id")
    .notNull()
    .references(() => borrower_workflows.id),
  vapi_call_id: varchar("vapi_call_id"),
  call_recording_url: varchar("call_recording_url"),
  call_transcript: text("call_transcript"),
  lump_sum_offered: numeric("lump_sum_offered"),
  lump_sum_discount_pct: numeric("lump_sum_discount_pct"),
  emi_amount: numeric("emi_amount"),
  emi_months: integer("emi_months"),
  emi_start_date: timestamp("emi_start_date"),
  hardship_offered: boolean("hardship_offered").default(false),
  offer_accepted: boolean("offer_accepted"),
  accepted_offer_type: varchar("accepted_offer_type"),
  objections_raised: json("objections_raised").default([]),
  call_duration_seconds: integer("call_duration_seconds"),
  created_at: timestamp("created_at").defaultNow().notNull(),
});

export const agent_conversations = pgTable("agent_conversations", {
  id: varchar("id").primaryKey().notNull(),
  workflow_id: varchar("workflow_id")
    .notNull()
    .references(() => borrower_workflows.id),
  user_id: varchar("user_id")
    .notNull()
    .references(() => users.id),
  agent_id: agentIdEnum("agent_id").notNull(),
  is_simulated: boolean("is_simulated").default(false),
  persona_type: personaEnum("persona_type"), // set for simulated convos
  seed: varchar("seed"), // for reproducibility
  prompt_version: integer("prompt_version").notNull(),
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
  user_id: varchar("user_id")
    .notNull()
    .references(() => users.id),
  agent_id: agentIdEnum("agent_id").notNull(),
  role: messageRoleEnum("role").notNull(),
  content: text("content").notNull(),
  token_count: integer("token_count"),
  created_at: timestamp("created_at").defaultNow().notNull(),
});

export const user_memories = pgTable("user_memories", {
  id: varchar("id").primaryKey().notNull(),
  user_id: varchar("user_id")
    .notNull()
    .references(() => users.id),
  memory_toc: json("memory_toc").default({}),
  memory_tree: json("memory_tree").default({}),
  token_estimate: integer("token_estimate").default(0),
  updated_at: timestamp("updated_at").defaultNow().notNull(),
});

export const prompt_versions = pgTable("prompt_versions", {
  id: varchar("id").primaryKey().notNull(),
  agent_id: agentIdEnum("agent_id").notNull(),
  version_number: integer("version_number").notNull(),
  prompt_text: text("prompt_text").notNull(),
  is_active: boolean("is_active").default(false).notNull(),
  adopted_at: timestamp("adopted_at"),
  retired_at: timestamp("retired_at"),
  adoption_reason: text("adoption_reason"),
  rejection_reason: text("rejection_reason"),
  created_at: timestamp("created_at").defaultNow().notNull(),
});

// Per-conversation scores (raw numbers for report)
// This is the core scientific data table.
// Every row = one scored conversation.
// metric_scores holds all individual metric values.
// Distribution stats are computed at query time, not stored here —
// that way rerun always reflects actual data.

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

  // Composite
  composite_score: numeric("composite_score").notNull(), // 0-100

  // Per-metric raw scores — stored flat for easy SQL aggregation
  // ARIA metrics
  score_identity_verified: numeric("score_identity_verified"), // 0 or 10
  score_info_completeness: numeric("score_info_completeness"), // 0-10
  score_no_redundancy: numeric("score_no_redundancy"), // 0-10
  score_tone_appropriateness: numeric("score_tone_appropriateness"), // 0-10

  // NOVA metrics
  score_offer_clarity: numeric("score_offer_clarity"), // 0-10
  score_objection_handling: numeric("score_objection_handling"), // 0-10
  score_commitment_attempt: numeric("score_commitment_attempt"), // 0-10
  score_context_continuity: numeric("score_context_continuity"), // 0-10

  // DELTA metrics
  score_consequence_accuracy: numeric("score_consequence_accuracy"), // 0-10
  score_deadline_specificity: numeric("score_deadline_specificity"), // 0-10
  score_no_negotiation_drift: numeric("score_no_negotiation_drift"), // 0-10

  // Shared
  score_compliance_pass: numeric("score_compliance_pass"), // 0 or 10

  // Compliance detail — which rules passed/failed
  compliance_breakdown: jsonb("compliance_breakdown").default({}),
  // e.g. {"identity_disclosure": true, "no_false_threats": false, ...}

  compliance_passed: boolean("compliance_passed"),

  // Second judge scores (for disagreement detection)
  judge_b_composite: numeric("judge_b_composite"),
  judge_b_metric_scores: jsonb("judge_b_metric_scores").default({}),
  judge_disagreement_delta: numeric("judge_disagreement_delta"), // abs diff A vs B

  // LLM cost tracking for this evaluation call
  eval_cost_usd: numeric("eval_cost_usd"),
  eval_model_used: varchar("eval_model_used"),

  created_at: timestamp("created_at").defaultNow().notNull(),
});

export const prompt_experiments = pgTable("prompt_experiments", {
  id: varchar("id").primaryKey().notNull(),
  agent_id: agentIdEnum("agent_id").notNull(),
  control_version: integer("control_version").notNull(),
  candidate_version: integer("candidate_version").notNull(),

  // Control batch stats
  control_n: integer("control_n").notNull(),
  control_mean: numeric("control_mean").notNull(),
  control_stddev: numeric("control_stddev").notNull(),
  control_median: numeric("control_median").notNull(),
  control_p10: numeric("control_p10"), // 10th percentile
  control_p90: numeric("control_p90"), // 90th percentile
  control_min: numeric("control_min"),
  control_max: numeric("control_max"),
  control_compliance_rate: numeric("control_compliance_rate"), // 0.0-1.0

  // Treatment batch stats
  treatment_n: integer("treatment_n").notNull(),
  treatment_mean: numeric("treatment_mean").notNull(),
  treatment_stddev: numeric("treatment_stddev").notNull(),
  treatment_median: numeric("treatment_median").notNull(),
  treatment_p10: numeric("treatment_p10"),
  treatment_p90: numeric("treatment_p90"),
  treatment_min: numeric("treatment_min"),
  treatment_max: numeric("treatment_max"),
  treatment_compliance_rate: numeric("treatment_compliance_rate"),

  // Test results
  mean_delta: numeric("mean_delta").notNull(), // treatment - control
  p_value: numeric("p_value").notNull(), // Welch t-test
  cohens_d: numeric("cohens_d"), // effect size
  is_significant: boolean("is_significant"), // p < 0.05

  // Decision
  adopted: boolean("adopted").notNull(),
  rejection_reason: text("rejection_reason"),
  // e.g. "p=0.12, not significant" or "compliance rate dropped 0.9->0.7"

  // Raw score arrays for full reproducibility
  control_scores: jsonb("control_scores").default([]), // [72, 68, 81, ...]
  treatment_scores: jsonb("treatment_scores").default([]), // [75, 79, 83, ...]

  // Cost of running this experiment
  experiment_cost_usd: numeric("experiment_cost_usd"),

  created_at: timestamp("created_at").defaultNow().notNull(),
});

export const meta_flags = pgTable("meta_flags", {
  id: varchar("id").primaryKey().notNull(),
  flag_type: flagTypeEnum("flag_type").notNull(),
  agent_id: agentIdEnum("agent_id"),

  // The numbers that triggered this flag — structured
  evidence: jsonb("evidence").default({}),
  // score_inflation:    { mean: 81.2, stddev: 7.3, sample_n: 30 }
  // metric_uselessness: { metric: "tone_appropriateness", stddev: 2.1 }
  // judge_disagreement: { metric: "objection_handling", pct_diverged: 0.31, avg_delta: 24.1 }
  // compliance_blindspot: { canary_id: "...", rule: "no_false_threats", missed: true }
  // post_adoption_regression: { old_mean: 71.2, new_mean: 63.4, delta: -7.8 }

  proposed_action: text("proposed_action"),
  resolved: boolean("resolved").default(false),
  resolution: text("resolution"),
  evaluator_version_before: integer("evaluator_version_before"),
  evaluator_version_after: integer("evaluator_version_after"),
  created_at: timestamp("created_at").defaultNow().notNull(),
  resolved_at: timestamp("resolved_at"),
});

export const evaluator_versions = pgTable("evaluator_versions", {
  id: varchar("id").primaryKey().notNull(),
  version_number: integer("version_number").notNull(),
  agent_id: agentIdEnum("agent_id").notNull(),
  judge_prompt: text("judge_prompt").notNull(),
  is_active: boolean("is_active").default(false),
  change_reason: text("change_reason"),
  triggered_by_flag_id: varchar("triggered_by_flag_id").references(
    () => meta_flags.id,
  ), // links evaluator change to the flag that caused it
  created_at: timestamp("created_at").defaultNow().notNull(),
});

export const compliance_canaries = pgTable("compliance_canaries", {
  id: varchar("id").primaryKey().notNull(),
  agent_id: agentIdEnum("agent_id").notNull(),
  rule: complianceRuleEnum("rule").notNull(), // hard-coded to 8 rules
  description: text("description").notNull(),
  transcript: text("transcript").notNull(),
  should_fail: boolean("should_fail").default(true),
  created_at: timestamp("created_at").defaultNow().notNull(),
});

export const canary_results = pgTable("canary_results", {
  id: varchar("id").primaryKey().notNull(),
  canary_id: varchar("canary_id")
    .notNull()
    .references(() => compliance_canaries.id),
  evaluator_version: integer("evaluator_version").notNull(),
  checker_result: boolean("checker_result"), // what checker returned
  correctly_flagged: boolean("correctly_flagged"), // did it match should_fail?
  created_at: timestamp("created_at").defaultNow().notNull(),
});

export const llm_cost_log = pgTable("llm_cost_log", {
  id: varchar("id").primaryKey().notNull(),
  call_type: varchar("call_type").notNull(),
  // "simulation" | "evaluation" | "meta_evaluation" | "prompt_generation"
  // "summarization" | "compliance_check" | "agent_response"
  agent_id: agentIdEnum("agent_id"),
  model_used: varchar("model_used").notNull(), // "claude-3-5-haiku", "gpt-4o" etc
  prompt_tokens: integer("prompt_tokens").notNull(),
  completion_tokens: integer("completion_tokens").notNull(),
  total_tokens: integer("total_tokens").notNull(),
  cost_usd: numeric("cost_usd").notNull(),
  conversation_id: varchar("conversation_id"),
  experiment_id: varchar("experiment_id"),
  created_at: timestamp("created_at").defaultNow().notNull(),
});

export const eval_runs = pgTable("eval_runs", {
  id: varchar("id").primaryKey().notNull(),
  run_label: varchar("run_label").notNull(), // "baseline", "v2_experiment" etc
  seed: integer("seed").notNull(),
  batch_size: integer("batch_size").notNull(),
  personas_used: jsonb("personas_used").default([]), // ["cooperative","combative",...]
  agent_ids: jsonb("agent_ids").default([]),
  prompt_versions_used: jsonb("prompt_versions_used").default({}),
  evaluator_versions_used: jsonb("evaluator_versions_used").default({}),
  total_conversations: integer("total_conversations"),
  total_cost_usd: numeric("total_cost_usd"),
  started_at: timestamp("started_at").defaultNow().notNull(),
  completed_at: timestamp("completed_at"),
  // Full config snapshot so reruns are identical
  config_snapshot: jsonb("config_snapshot").default({}),
});
