"use server";

import { auth } from "@clerk/nextjs/server";
import { revalidatePath } from "next/cache";

const apiBase = process.env.API_URL ?? "http://localhost:9000";
const clerkJwtTemplate =
  process.env.CLERK_JWT_TEMPLATE ?? process.env.NEXT_PUBLIC_CLERK_JWT_TEMPLATE;

async function backendHeaders(): Promise<HeadersInit> {
  const authState = await auth();
  const token = await authState.getToken(
    clerkJwtTemplate ? { template: clerkJwtTemplate } : undefined,
  );
  return {
    "content-type": "application/json",
    ...(token ? { authorization: `Bearer ${token}` } : {}),
  };
}

async function backendJson<T>(path: string, init?: RequestInit): Promise<T | null> {
  const res = await fetch(`${apiBase}${path}`, {
    ...init,
    headers: {
      ...(await backendHeaders()),
      ...(init?.headers ?? {}),
    },
    cache: "no-store",
  });
  if (!res.ok) {
    return null;
  }
  return (await res.json()) as T;
}

export async function loadAdminEvalAction() {
  return backendJson<AdminEvalSummary>("/api/v1/admin/eval");
}

export async function loadAdminMetricsAction() {
  return backendJson<AdminEvalMetrics>("/api/v1/admin/eval/metrics");
}

export async function loadAdminMetaAction() {
  return backendJson<AdminEvalMeta>("/api/v1/admin/eval/meta");
}

export async function runAdminFullCycleAction() {
  const result = await backendJson<AdminEvalStartResult>("/api/v1/admin/eval/full-cycle/start", {
    method: "POST",
    body: JSON.stringify({
      reset: false,
      seed: 42,
      batch_size: 1,
      agents: ["aria", "nova", "delta"],
      personas: ["cooperative", "combative", "evasive", "distressed", "confused"],
      max_turns_per_agent: 6,
      max_cost_usd: 15,
      max_prompt_iterations: 4,
      meta_eval_every_judge_runs: 6,
    }),
  });
  return result;
}

export async function resetAdminEvalDataAction() {
  const result = await backendJson<{ ok: boolean; message: string }>("/api/v1/admin/eval/reset", {
    method: "POST",
    body: JSON.stringify({}),
  });
  revalidatePath("/admin");
  return result;
}

export async function loadAdminEvalProgressAction(runId?: string | null) {
  return backendJson<AdminEvalProgress>(`/api/v1/admin/eval/runs/${runId || "latest"}`);
}

export type AgentId = "aria" | "nova" | "delta";

export type ConversationScore = {
  id: string;
  conversation_id: string;
  workflow_id?: string | null;
  prompt_version: number;
  evaluator_version: number;
  is_simulated?: boolean | null;
  persona_type?: string | null;
  seed?: string | null;
  composite_score: number;
  score_compliance_pass?: number | null;
  compliance_passed?: boolean | null;
  judge_disagreement_delta?: number | null;
  eval_cost_usd?: number | null;
  eval_model_used?: string | null;
  created_at: string;
};

export type AgentConversation = {
  id: string;
  workflow_id: string;
  user_id: string;
  agent_id: AgentId;
  is_simulated?: boolean | null;
  persona_type?: string | null;
  seed?: string | null;
  prompt_version: number;
  outcome?: string | null;
  total_turns?: number | null;
  total_tokens_used?: number | null;
  started_at: string;
  ended_at?: string | null;
};

export type AgentMessage = {
  id: string;
  conversation_id: string;
  workflow_id: string;
  agent_id: AgentId;
  role: "borrower" | "agent" | "tool" | "system";
  content: string;
  token_count?: number | null;
  created_at: string;
};

export type PromptExperiment = {
  id: string;
  agent_id: AgentId;
  control_version: number;
  candidate_version: number;
  control_n: number;
  control_mean: number;
  control_stddev: number;
  control_median: number;
  control_compliance_rate: number;
  control_scores?: number[];
  treatment_n: number;
  treatment_mean: number;
  treatment_stddev: number;
  treatment_median: number;
  treatment_compliance_rate: number;
  treatment_scores?: number[];
  mean_delta: number;
  p_value: number;
  cohens_d?: number | null;
  is_significant?: boolean | null;
  adopted: boolean;
  rejection_reason?: string | null;
  experiment_cost_usd?: number | null;
  created_at: string;
};

export type PromptVersion = {
  id: string;
  agent_id: AgentId;
  version_number: number;
  prompt_text: string;
  is_active: boolean;
  adopted_at?: string | null;
  retired_at?: string | null;
  adoption_reason?: string | null;
  rejection_reason?: string | null;
  created_at: string;
};

export type LlmCostLog = {
  id: string;
  call_type: string;
  agent_id?: AgentId | null;
  model_used: string;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  cost_usd: number;
  conversation_id?: string | null;
  experiment_id?: string | null;
  created_at: string;
};

export type MetaFlag = {
  id: string;
  flag_type: string;
  agent_id?: AgentId | null;
  evidence?: Record<string, unknown>;
  proposed_action?: string | null;
  resolved?: boolean | null;
  resolution?: string | null;
  evaluator_version_before?: number | null;
  evaluator_version_after?: number | null;
  created_at: string;
  resolved_at?: string | null;
};

export type EvaluatorVersion = {
  id: string;
  version_number: number;
  agent_id: AgentId;
  judge_prompt: string;
  is_active?: boolean | null;
  change_reason?: string | null;
  triggered_by_flag_id?: string | null;
  created_at: string;
};

export type CanaryResult = {
  id: string;
  canary_id: string;
  evaluator_version: number;
  checker_result?: boolean | null;
  correctly_flagged?: boolean | null;
  created_at: string;
};

export type AdminEvalSummary = {
  conversation_scores: ConversationScore[];
  prompt_experiments: PromptExperiment[];
  cost_log: LlmCostLog[];
  prompt_versions: PromptVersion[];
  meta_flags: MetaFlag[];
  evaluator_versions: EvaluatorVersion[];
  canary_results: CanaryResult[];
  total_cost_usd: number;
};

export type AdminEvalRun = {
  id: string;
  status: "running" | "completed" | "failed";
  config: Record<string, unknown>;
  started_at: string;
  completed_at?: string | null;
  error?: string | null;
};

export type AdminEvalStartResult = {
  run_id: string;
  existing: boolean;
  run: AdminEvalRun;
};

export type AdminConversationPreview = {
  conversation: AgentConversation;
  messages: AgentMessage[];
  score?: ConversationScore | null;
};

export type AdminEvalProgress = {
  run?: AdminEvalRun | null;
  counts: {
    conversations: number;
    messages: number;
    scores: number;
    prompt_experiments: number;
    cost_logs: number;
  };
  total_cost_usd: number;
  recent_scores: ConversationScore[];
  experiments: PromptExperiment[];
  conversations: AdminConversationPreview[];
  last_generated_at: string;
};

export type MetricAggregate = {
  n: number;
  mean: number;
  stddev: number;
  median: number;
  compliance_rate: number;
};

export type AdminEvalMetrics = {
  total_scores: number;
  total_cost_usd: number;
  system_aggregate: MetricAggregate;
  prompt_experiments: PromptExperiment[];
};

export type AdminEvalMeta = {
  meta_flags: MetaFlag[];
  evaluator_versions: EvaluatorVersion[];
  compliance_canaries: unknown[];
  canary_results: CanaryResult[];
};
