"use server";

import { auth } from "@clerk/nextjs/server";

const apiBase = process.env.API_URL ?? process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:9000";
const clerkJwtTemplate = process.env.CLERK_JWT_TEMPLATE ?? process.env.NEXT_PUBLIC_CLERK_JWT_TEMPLATE;

async function backendHeaders(): Promise<HeadersInit> {
  const authState = await auth();
  const token = await authState.getToken(clerkJwtTemplate ? { template: clerkJwtTemplate } : undefined);
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

export async function startWorkflowAction() {
  return backendJson("/api/v1/workflows/start", {
    method: "POST",
    body: JSON.stringify({}),
  });
}

export async function loadConversationAction(id: string) {
  return backendJson(`/api/v1/conversations/${id}`);
}

export async function sendChatMessageAction(workflowId: string, message: string) {
  return backendJson(`/api/v1/chat/${workflowId}`, {
    method: "POST",
    body: JSON.stringify({ message }),
  });
}
