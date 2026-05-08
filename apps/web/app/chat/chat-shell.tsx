"use client";

import * as React from "react";
import { Bot, Send, ShieldCheck } from "lucide-react";

import { Button } from "@/components/ui/button";
import { loadConversationAction, sendChatMessageAction, startWorkflowAction } from "./actions";

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

export default function ChatShell() {
  const [workflowId, setWorkflowId] = React.useState<string>("");
  const [messages, setMessages] = React.useState<AgentMessage[]>([]);
  const [input, setInput] = React.useState("");
  const [isLoading, setIsLoading] = React.useState(false);
  const [stage, setStage] = React.useState<"aria" | "nova" | "delta">("aria");
  const scrollRef = React.useRef<HTMLDivElement | null>(null);
  const optimisticIdRef = React.useRef(0);
  const didStartRef = React.useRef(false);
  const activeChatAgent = stage === "delta" ? "delta" : "aria";
  const activeChatLabel = stage === "delta" ? "Riverline Final Notice" : "Riverline";

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
    const data = await startWorkflowAction() as { workflow?: { id: string; current_stage?: "aria" | "nova" | "delta" }; existing?: boolean } | null;
    if (!data?.workflow) {
      setIsLoading(false);
      return;
    }
    const id = data.workflow.id as string;
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

  const sendMessage = React.useCallback(async () => {
    const message = input.trim();
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
  }, [activeChatAgent, input, isLoading, loadConversation, workflowId]);

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
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: "smooth" });
  }, [messages]);

  return (
    <main className="min-h-screen bg-[#f4efe4] text-[#18211d]">
      <div className="mx-auto flex min-h-screen max-w-5xl flex-col px-4 py-6">
        <header className="mb-4 rounded-[2rem] border border-[#24382f]/10 bg-[#fffaf0]/80 p-5 shadow-[0_24px_80px_rgba(46,35,18,0.12)] backdrop-blur">
          <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
            <div>
              <p className="text-xs font-semibold uppercase tracking-[0.28em] text-[#7b4d25]">
                Riverline Collections AI
              </p>
              <h1 className="mt-2 text-3xl font-semibold tracking-[-0.04em] text-[#17231d]">
                Borrower Resolution Chat
              </h1>
            </div>
            <div className="flex items-center gap-2 rounded-full bg-[#17231d] px-4 py-2 text-sm font-medium text-[#fffaf0]">
              <Bot className="size-4" />
              Active chat: {activeChatLabel}
            </div>
          </div>
          <div className="mt-5 flex items-start gap-3 rounded-2xl border border-[#c68632]/25 bg-[#fff3d4] px-4 py-3 text-sm text-[#5f3c18]">
            <ShieldCheck className="mt-0.5 size-4 shrink-0" />
            This conversation is with an AI agent and is being recorded.
          </div>
        </header>

        <section className="grid min-h-0 flex-1 rounded-[2rem] border border-[#24382f]/10 bg-[#fffcf4] shadow-[0_24px_80px_rgba(46,35,18,0.12)]">
          <div ref={scrollRef} className="min-h-[55vh] overflow-y-auto p-4 md:p-6">
            <div className="space-y-4">
              {messages.map((message) => (
                <div
                  key={message.id}
                  className={`flex ${message.role === "borrower" ? "justify-end" : "justify-start"}`}
                >
                  <div
                    className={`max-w-[82%] rounded-[1.5rem] px-4 py-3 text-sm leading-6 md:max-w-[68%] ${
                      message.role === "borrower"
                        ? "bg-[#17231d] text-[#fffaf0]"
                        : "border border-[#24382f]/10 bg-[#f1eadb] text-[#17231d]"
                    }`}
                  >
                    <div className="mb-1 text-[10px] font-bold uppercase tracking-[0.18em] opacity-60">
                      {message.role === "borrower" ? "Borrower" : "Riverline"}
                    </div>
                    {message.content}
                  </div>
                </div>
              ))}
            </div>
          </div>

          <div className="border-t border-[#24382f]/10 p-4">
            <div className="flex gap-3">
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
                className="min-h-12 flex-1 resize-none rounded-2xl border border-[#24382f]/15 bg-white px-4 py-3 text-sm outline-none ring-[#c68632]/30 transition focus:ring-4"
              />
              <Button
                disabled={isLoading || !workflowId}
                onClick={() => void sendMessage()}
                className="h-auto rounded-2xl bg-[#c15f2e] px-5 text-white hover:bg-[#9d4924]"
              >
                <Send className="size-4" />
              </Button>
            </div>
          </div>
        </section>
      </div>
    </main>
  );
}
