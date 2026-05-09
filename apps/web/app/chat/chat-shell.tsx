"use client";

import * as React from "react";
import { UserButton } from "@clerk/nextjs";
import {
  Bot,
  CheckCircle2,
  Clock3,
  FileText,
  MessageSquarePlus,
  MoreHorizontal,
  PanelLeft,
  PhoneCall,
  Search,
  Send,
  ShieldCheck,
  Sparkles,
  Zap,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  loadConversationAction,
  sendChatMessageAction,
  startWorkflowAction,
} from "./actions";

type AgentMessage = {
  id: string;
  role: "agent" | "borrower";
  content: string;
  agent_id: "aria" | "nova" | "delta";
  created_at: string;
};

type ConversationView = {
  workflow?: {
    id: string;
    current_stage: "aria" | "nova" | "delta";
    outcome?: string | null;
    final_offer_deadline?: string | null;
  };
  messages?: AgentMessage[];
};

const stageCopy = {
  aria: {
    title: "Assessment chat",
    subtitle: "Verify identity, collect context, and schedule the call.",
    badge: "Chat active",
  },
  nova: {
    title: "Resolution call pending",
    subtitle: "Phone resolution is in progress. Chat remains available here.",
    badge: "Call stage",
  },
  delta: {
    title: "Final notice chat",
    subtitle: "Final documented offer and deadline support.",
    badge: "Final stage",
  },
} as const;

const suggestions = [
  "I am ready to verify my account.",
  "I need to change my call time.",
  "What happens next?",
  "I want to discuss hardship.",
];

const rails = [
  "Current account workflow",
  "Resolution call status",
  "Final notice record",
];

export default function ChatShell() {
  const [workflowId, setWorkflowId] = React.useState("");
  const [messages, setMessages] = React.useState<AgentMessage[]>([]);
  const [input, setInput] = React.useState("");
  const [isLoading, setIsLoading] = React.useState(false);
  const [stage, setStage] = React.useState<"aria" | "nova" | "delta">("aria");
  const scrollRef = React.useRef<HTMLDivElement | null>(null);
  const optimisticIdRef = React.useRef(0);
  const didStartRef = React.useRef(false);
  const activeChatAgent = stage === "delta" ? "delta" : "aria";
  const stageMeta = stageCopy[stage];

  const loadConversation = React.useCallback(async (id: string) => {
    const data = (await loadConversationAction(id)) as ConversationView | null;
    if (!data) return;
    setStage(data.workflow?.current_stage ?? "aria");
    if (data.messages?.length) {
      setMessages(data.messages);
    }
  }, []);

  const startWorkflow = React.useCallback(async () => {
    setIsLoading(true);
    const data = (await startWorkflowAction()) as {
      workflow?: { id: string; current_stage?: "aria" | "nova" | "delta" };
      existing?: boolean;
    } | null;
    if (!data?.workflow) {
      setIsLoading(false);
      return;
    }
    const id = data.workflow.id;
    setWorkflowId(id);
    setStage(data.workflow.current_stage ?? "aria");
    if (data.existing) {
      await loadConversation(id);
      setIsLoading(false);
      return;
    }
    setMessages([
      {
        id: "local-welcome",
        role: "agent",
        agent_id: "aria",
        content:
          "I am Riverline's AI assistant. This conversation is being recorded. Please confirm when you are ready to verify the account details on file.",
        created_at: new Date().toISOString(),
      },
    ]);
    setIsLoading(false);
  }, [loadConversation]);

  const sendMessage = React.useCallback(
    async (value = input) => {
      const message = value.trim();
      if (!message || !workflowId || isLoading) return;
      setInput("");
      setIsLoading(true);
      optimisticIdRef.current += 1;
      const optimistic: AgentMessage = {
        id: `local-${optimisticIdRef.current}`,
        role: "borrower",
        agent_id: activeChatAgent,
        content: message,
        created_at: new Date().toISOString(),
      };
      setMessages((prev) => [...prev, optimistic]);
      const res = await sendChatMessageAction(workflowId, message);
      if (res) {
        await loadConversation(workflowId);
      }
      setIsLoading(false);
    },
    [activeChatAgent, input, isLoading, loadConversation, workflowId],
  );

  React.useEffect(() => {
    if (didStartRef.current) return;
    didStartRef.current = true;
    const timer = window.setTimeout(() => {
      void startWorkflow();
    }, 0);
    return () => window.clearTimeout(timer);
  }, [startWorkflow]);

  React.useEffect(() => {
    if (!workflowId) return;
    const timer = window.setInterval(() => {
      void loadConversation(workflowId);
    }, 2000);
    return () => window.clearInterval(timer);
  }, [loadConversation, workflowId]);

  React.useEffect(() => {
    scrollRef.current?.scrollTo({
      top: scrollRef.current.scrollHeight,
      behavior: "smooth",
    });
  }, [messages]);

  return (
    <div className="relative flex h-screen overflow-hidden bg-background text-foreground">
      <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(circle_at_18%_12%,rgba(236,72,153,0.2),transparent_28%),radial-gradient(circle_at_85%_18%,rgba(217,70,239,0.18),transparent_30%),linear-gradient(180deg,rgba(255,255,255,0.03),transparent_32%)]" />

      <aside className="relative hidden w-72 shrink-0 border-r border-pink-300/10 bg-black/35 p-4 backdrop-blur-2xl lg:flex lg:flex-col">
        <div className="flex items-center gap-3 px-2 py-2">
          <div className="flex size-10 items-center justify-center rounded-2xl bg-pink-500 text-white shadow-[0_0_34px_rgba(236,72,153,0.55)]">
            <Sparkles className="size-5" />
          </div>
          <div>
            <p className="text-sm font-semibold text-pink-50">Riverline</p>
            <p className="text-xs text-zinc-500">Secure collections workspace</p>
          </div>
        </div>

        <Button
          className="mt-5 w-full rounded-2xl bg-pink-500 text-white shadow-[0_0_30px_rgba(236,72,153,0.35)] hover:bg-pink-400"
          onClick={() => void startWorkflow()}
          disabled={isLoading}
        >
          <MessageSquarePlus className="size-4" />
          Refresh workflow
        </Button>

        <div className="mt-4 flex items-center gap-2 rounded-2xl border border-white/10 bg-white/[0.03] px-3 py-2 text-sm text-zinc-500">
          <Search className="size-4" />
          Workflow lookup
          <kbd className="ml-auto rounded-md border border-white/10 px-1.5 py-0.5 text-[10px] text-zinc-500">
            live
          </kbd>
        </div>

        <div className="mt-6 space-y-1">
          <p className="px-2 text-xs font-medium uppercase tracking-[0.18em] text-zinc-600">
            Journey
          </p>
          {rails.map((thread, index) => (
            <div
              key={thread}
              className={`flex w-full items-center gap-3 rounded-2xl px-3 py-2.5 text-left text-sm ${
                index === (stage === "aria" ? 0 : stage === "nova" ? 1 : 2)
                  ? "bg-pink-400/12 text-pink-50"
                  : "text-zinc-400"
              }`}
            >
              {index === 1 ? <PhoneCall className="size-4" /> : <FileText className="size-4" />}
              <span className="truncate">{thread}</span>
            </div>
          ))}
        </div>

        <div className="mt-auto rounded-3xl border border-pink-300/15 bg-pink-400/10 p-4">
          <div className="flex items-center gap-2 text-sm font-medium text-pink-50">
            <Zap className="size-4 text-pink-300" />
            Live integration
          </div>
          <p className="mt-2 text-xs leading-5 text-zinc-400">
            This UI is connected to the current borrower workflow. ARIA handles
            chat until DELTA becomes active.
          </p>
        </div>
      </aside>

      <section className="relative flex min-w-0 flex-1 flex-col">
        <header className="flex h-16 shrink-0 items-center justify-between border-b border-pink-300/10 bg-black/20 px-4 backdrop-blur-xl md:px-6">
          <div className="flex items-center gap-3">
            <Button
              size="icon"
              variant="ghost"
              className="rounded-full text-zinc-400 hover:bg-pink-400/10 hover:text-pink-100 lg:hidden"
            >
              <PanelLeft className="size-5" />
              <span className="sr-only">Open sidebar</span>
            </Button>
            <div>
              <div className="flex items-center gap-2">
                <h1 className="text-sm font-semibold text-pink-50 md:text-base">
                  {stageMeta.title}
                </h1>
                <span className="rounded-full border border-emerald-300/20 bg-emerald-400/10 px-2 py-0.5 text-[11px] text-emerald-200">
                  {stageMeta.badge}
                </span>
              </div>
              <p className="text-xs text-zinc-500">{stageMeta.subtitle}</p>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <Button
              variant="secondary"
              size="sm"
              className="hidden rounded-full border border-pink-300/15 bg-white/[0.04] text-pink-100 hover:bg-pink-400/10 md:inline-flex"
            >
              <Bot className="size-4" />
              Riverline AI
            </Button>
            <UserButton
              appearance={{
                elements: {
                  userButtonAvatarBox:
                    "h-9 w-9 ring-2 ring-pink-300/30 shadow-[0_0_24px_rgba(236,72,153,0.25)]",
                },
              }}
            />
          </div>
        </header>

        <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto px-4 py-6 md:px-8">
          <div className="mx-auto flex max-w-4xl flex-col gap-5">
            <div className="rounded-3xl border border-pink-300/15 bg-zinc-950/55 p-4 shadow-2xl shadow-pink-950/10 backdrop-blur">
              <div className="flex flex-wrap items-center gap-3 text-xs text-zinc-400">
                <span className="inline-flex items-center gap-1.5 text-pink-100">
                  <ShieldCheck className="size-3.5 text-pink-300" />
                  AI disclosure and logging active
                </span>
                <span className="hidden h-1 w-1 rounded-full bg-zinc-700 sm:block" />
                <span className="inline-flex items-center gap-1.5">
                  <Clock3 className="size-3.5" />
                  Workflow {workflowId ? workflowId.slice(0, 10) : "starting"}
                </span>
              </div>
            </div>

            {messages.map((message) => (
              <div
                key={message.id}
                className={`flex ${message.role === "borrower" ? "justify-end" : "justify-start"}`}
              >
                <div
                  className={`group max-w-[86%] rounded-[1.6rem] border px-4 py-3 text-sm leading-6 shadow-2xl backdrop-blur md:max-w-[72%] ${
                    message.role === "borrower"
                      ? "border-pink-300/20 bg-pink-500 text-white shadow-pink-950/25"
                      : "border-white/10 bg-zinc-950/75 text-zinc-100 shadow-black/20"
                  }`}
                >
                  <div className="mb-1 flex items-center justify-between gap-3 text-[10px] font-bold uppercase tracking-[0.18em] opacity-65">
                    <span>{message.role === "borrower" ? "Borrower" : "Riverline"}</span>
                    <span>{new Date(message.created_at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}</span>
                  </div>
                  <p className="whitespace-pre-wrap">{message.content}</p>
                </div>
              </div>
            ))}

            {isLoading ? (
              <div className="flex justify-start">
                <div className="inline-flex items-center gap-3 rounded-[1.6rem] border border-white/10 bg-zinc-950/75 px-4 py-3 text-sm text-zinc-300 shadow-2xl shadow-black/20 backdrop-blur">
                  <span className="flex gap-1">
                    <span className="size-1.5 animate-bounce rounded-full bg-pink-300 [animation-delay:-0.2s]" />
                    <span className="size-1.5 animate-bounce rounded-full bg-pink-300 [animation-delay:-0.1s]" />
                    <span className="size-1.5 animate-bounce rounded-full bg-pink-300" />
                  </span>
                  Riverline is responding...
                </div>
              </div>
            ) : null}
          </div>
        </div>

        <div className="shrink-0 border-t border-pink-300/10 bg-background/80 px-4 py-4 backdrop-blur-xl md:px-8">
          <div className="mx-auto max-w-4xl">
            <div className="mb-3 flex gap-2 overflow-x-auto pb-1">
              {suggestions.map((suggestion) => (
                <button
                  key={suggestion}
                  disabled={isLoading || !workflowId}
                  onClick={() => void sendMessage(suggestion)}
                  className="shrink-0 rounded-full border border-pink-300/15 bg-white/[0.04] px-3 py-2 text-xs font-medium text-pink-100 transition hover:bg-pink-400/10 disabled:cursor-not-allowed disabled:opacity-45"
                >
                  {suggestion}
                </button>
              ))}
            </div>
            <div className="rounded-[1.75rem] border border-pink-300/15 bg-zinc-950/80 p-2 shadow-2xl shadow-pink-950/20 backdrop-blur-xl">
              <div className="flex items-end gap-2">
                <textarea
                  value={input}
                  onChange={(event) => setInput(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter" && !event.shiftKey) {
                      event.preventDefault();
                      void sendMessage();
                    }
                  }}
                  placeholder="Type your response..."
                  className="min-h-12 flex-1 resize-none rounded-[1.25rem] border-0 bg-transparent px-3 py-3 text-sm text-pink-50 outline-none placeholder:text-zinc-600"
                />
                <Button
                  disabled={isLoading || !workflowId}
                  onClick={() => void sendMessage()}
                  className="size-12 shrink-0 rounded-2xl bg-pink-500 text-white shadow-[0_0_28px_rgba(236,72,153,0.35)] hover:bg-pink-400"
                >
                  <Send className="size-4" />
                </Button>
              </div>
            </div>
            <div className="mt-3 flex items-center justify-between px-1 text-xs text-zinc-600">
              <span className="inline-flex items-center gap-1.5">
                <CheckCircle2 className="size-3.5 text-pink-300" />
                Protected by Clerk
              </span>
              <span className="inline-flex items-center gap-1 text-zinc-500">
                <MoreHorizontal className="size-4" />
                Stage: {stage.toUpperCase()}
              </span>
            </div>
          </div>
        </div>
      </section>
    </div>
  );
}
