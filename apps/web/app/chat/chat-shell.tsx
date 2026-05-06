"use client";

import * as React from "react";
import { UserButton } from "@clerk/nextjs";
import {
  Bot,
  CheckCircle2,
  Command,
  FileText,
  ImageIcon,
  MessageSquarePlus,
  MoreHorizontal,
  PanelLeft,
  Search,
  Settings2,
  Sparkles,
  Zap,
} from "lucide-react";

import { Conversation, ConversationEmpty } from "@/components/ai/conversation";
import { AiLoader } from "@/components/ai/loader";
import { Message, MessageActions, MessageContent } from "@/components/ai/message";
import {
  formatFileSize,
  PromptAttachment,
  PromptInput,
} from "@/components/ai/prompt-input";
import { Reasoning } from "@/components/ai/reasoning";
import { Sources } from "@/components/ai/sources";
import { Suggestion } from "@/components/ai/suggestion";
import { Button } from "@/components/ui/button";

type ChatMessage = {
  id: string;
  role: "user" | "assistant";
  content: string;
  attachments?: PromptAttachment[];
  reasoning?: string;
  sources?: { title: string; url: string }[];
};

const starterMessages: ChatMessage[] = [
  {
    id: "welcome",
    role: "assistant",
    content:
      "Welcome back. I can help you pressure-test plans, summarize messy threads, draft polished docs, or turn scattered context into a clean decision.",
    reasoning:
      "I have your secure Riverline workspace open and will keep responses concise, structured, and actionable.",
    sources: [
      { title: "Workspace context", url: "#" },
      { title: "Security scope", url: "#" },
    ],
  },
];

const threads = [
  "Launch plan review",
  "Customer risk summary",
  "Sprint priorities",
  "Hiring scorecard",
];

const suggestions = [
  "Summarize today’s priorities",
  "Draft a customer-ready update",
  "Find risks in this plan",
  "Turn notes into action items",
];

export default function ChatShell() {
  const [messages, setMessages] = React.useState<ChatMessage[]>(starterMessages);
  const [input, setInput] = React.useState("");
  const [attachments, setAttachments] = React.useState<PromptAttachment[]>([]);
  const [isLoading, setIsLoading] = React.useState(false);
  const messageIdRef = React.useRef(0);
  const attachmentIdRef = React.useRef(0);
  const objectUrlsRef = React.useRef<Set<string>>(new Set());
  const responseTimeoutRef = React.useRef<number | null>(null);

  React.useEffect(() => {
    const objectUrls = objectUrlsRef.current;

    return () => {
      if (responseTimeoutRef.current) {
        window.clearTimeout(responseTimeoutRef.current);
      }
      objectUrls.forEach((url) => URL.revokeObjectURL(url));
    };
  }, []);

  const nextMessageId = (role: ChatMessage["role"]) => {
    messageIdRef.current += 1;
    return `${role}-${messageIdRef.current}`;
  };

  const addAttachments = (files: File[]) => {
    const nextAttachments = files.map((file) => {
      attachmentIdRef.current += 1;
      const url = file.type.startsWith("image/")
        ? URL.createObjectURL(file)
        : undefined;

      if (url) {
        objectUrlsRef.current.add(url);
      }

      return {
        id: `attachment-${attachmentIdRef.current}`,
        name: file.name,
        size: file.size,
        type: file.type || "application/octet-stream",
        url,
      };
    });

    setAttachments((prev) => [...prev, ...nextAttachments]);
  };

  const removeAttachment = (id: string) => {
    setAttachments((prev) => {
      const attachment = prev.find((item) => item.id === id);
      if (attachment?.url) {
        URL.revokeObjectURL(attachment.url);
        objectUrlsRef.current.delete(attachment.url);
      }

      return prev.filter((item) => item.id !== id);
    });
  };

  const sendMessage = (value = input) => {
    if (isLoading) return;

    const trimmed = value.trim();
    if (!trimmed && attachments.length === 0) return;
    const messageAttachments = attachments;

    const userMessage: ChatMessage = {
      id: nextMessageId("user"),
      role: "user",
      content: trimmed || "Attached files",
      attachments: messageAttachments,
    };

    setMessages((prev) => [...prev, userMessage]);
    setInput("");
    setAttachments([]);
    setIsLoading(true);

    responseTimeoutRef.current = window.setTimeout(() => {
      const attachmentSummary =
        messageAttachments.length > 0
          ? ` I see ${messageAttachments.length} attached file${
              messageAttachments.length === 1 ? "" : "s"
            } and I’ll treat them as part of the working context.`
          : "";
      const assistantMessage: ChatMessage = {
        id: nextMessageId("assistant"),
        role: "assistant",
        content:
          `Here’s a clean path forward: I’d separate the goal, constraints, risks, and next actions, then turn the highest-risk assumption into the first thing to validate.${attachmentSummary} If you share the source material, I can produce a tighter answer with exact decisions and owners.`,
        reasoning:
          "I interpreted your prompt as a planning request, identified the missing inputs, and shaped the response around an immediately usable operating structure.",
        sources: [
          { title: "Current thread", url: "#" },
          { title: "Riverline memory", url: "#" },
        ],
      };

      setMessages((prev) => [...prev, assistantMessage]);
      setIsLoading(false);
      responseTimeoutRef.current = null;
    }, 900);
  };

  const regenerateLast = () => {
    const lastUser = [...messages].reverse().find((message) => message.role === "user");
    if (!lastUser) return;

    setMessages((prev) => prev.filter((message) => message.role !== "assistant" || message.id === "welcome"));
    setInput(lastUser.content);
    setAttachments(lastUser.attachments ?? []);
  };

  const startNewThread = () => {
    if (responseTimeoutRef.current) {
      window.clearTimeout(responseTimeoutRef.current);
      responseTimeoutRef.current = null;
    }
    setMessages(starterMessages);
    setInput("");
    setAttachments([]);
    setIsLoading(false);
  };

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
            <p className="text-xs text-zinc-500">Private AI workspace</p>
          </div>
        </div>

        <Button
          className="mt-5 w-full rounded-2xl bg-pink-500 text-white shadow-[0_0_30px_rgba(236,72,153,0.35)] hover:bg-pink-400"
          onClick={startNewThread}
        >
          <MessageSquarePlus className="size-4" />
          New thread
        </Button>

        <div className="mt-4 flex items-center gap-2 rounded-2xl border border-white/10 bg-white/[0.03] px-3 py-2 text-sm text-zinc-500">
          <Search className="size-4" />
          Search threads
          <kbd className="ml-auto rounded-md border border-white/10 px-1.5 py-0.5 text-[10px] text-zinc-500">
            /
          </kbd>
        </div>

        <div className="mt-6 space-y-1">
          <p className="px-2 text-xs font-medium uppercase tracking-[0.18em] text-zinc-600">
            Recent
          </p>
          {threads.map((thread, index) => (
            <button
              key={thread}
              className={`flex w-full items-center gap-3 rounded-2xl px-3 py-2.5 text-left text-sm transition ${
                index === 0
                  ? "bg-pink-400/12 text-pink-50"
                  : "text-zinc-400 hover:bg-white/[0.04] hover:text-zinc-100"
              }`}
            >
              <FileText className="size-4" />
              <span className="truncate">{thread}</span>
            </button>
          ))}
        </div>

        <div className="mt-auto rounded-3xl border border-pink-300/15 bg-pink-400/10 p-4">
          <div className="flex items-center gap-2 text-sm font-medium text-pink-50">
            <Zap className="size-4 text-pink-300" />
            Pro context
          </div>
          <p className="mt-2 text-xs leading-5 text-zinc-400">
            118k token window, workspace memory, and source-aware replies are
            enabled for this session.
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
                  Launch plan review
                </h1>
                <span className="rounded-full border border-emerald-300/20 bg-emerald-400/10 px-2 py-0.5 text-[11px] text-emerald-200">
                  Live
                </span>
              </div>
              <p className="text-xs text-zinc-500">
                Riverline Muse · fast, private, source-aware
              </p>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <Button
              variant="secondary"
              size="sm"
              className="hidden rounded-full border border-pink-300/15 bg-white/[0.04] text-pink-100 hover:bg-pink-400/10 md:inline-flex"
            >
              <Bot className="size-4" />
              Muse 4.1
            </Button>
            <Button
              size="icon"
              variant="ghost"
              className="rounded-full text-zinc-400 hover:bg-pink-400/10 hover:text-pink-100"
            >
              <Settings2 className="size-4" />
              <span className="sr-only">Chat settings</span>
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

        <Conversation>
          {messages.length === 0 ? (
            <ConversationEmpty>
              <div>
                <div className="mx-auto flex size-14 items-center justify-center rounded-3xl bg-pink-500 text-white shadow-[0_0_44px_rgba(236,72,153,0.5)]">
                  <Command className="size-7" />
                </div>
                <h2 className="mt-5 text-2xl font-semibold text-pink-50">
                  What should we make sharper?
                </h2>
                <p className="mt-2 text-sm text-zinc-500">
                  Ask a question, drop notes, or start with a suggested prompt.
                </p>
              </div>
            </ConversationEmpty>
          ) : (
            messages.map((message) => (
              <Message key={message.id} from={message.role}>
                {message.reasoning ? (
                  <Reasoning title="Thinking trace">{message.reasoning}</Reasoning>
                ) : null}
                <MessageContent from={message.role}>{message.content}</MessageContent>
                {message.attachments && message.attachments.length > 0 ? (
                  <div className="grid gap-2 sm:grid-cols-2">
                    {message.attachments.map((attachment) => {
                      const isImage = attachment.type.startsWith("image/");

                      return (
                        <a
                          key={attachment.id}
                          href={attachment.url}
                          target="_blank"
                          rel="noreferrer"
                          className="group flex min-w-0 items-center gap-3 rounded-2xl border border-pink-300/15 bg-zinc-950/75 p-2 pr-3 text-left shadow-2xl shadow-black/20 backdrop-blur transition hover:border-pink-300/40 hover:bg-pink-400/10"
                        >
                          <div className="grid size-12 shrink-0 place-items-center overflow-hidden rounded-xl bg-pink-400/10 text-pink-200">
                            {isImage && attachment.url ? (
                              // eslint-disable-next-line @next/next/no-img-element
                              <img
                                src={attachment.url}
                                alt=""
                                className="h-full w-full object-cover"
                              />
                            ) : isImage ? (
                              <ImageIcon className="size-5" />
                            ) : (
                              <FileText className="size-5" />
                            )}
                          </div>
                          <div className="min-w-0 flex-1">
                            <p className="truncate text-sm font-medium text-pink-50">
                              {attachment.name}
                            </p>
                            <p className="text-xs text-zinc-500">
                              {attachment.type || "File"} ·{" "}
                              {formatFileSize(attachment.size)}
                            </p>
                          </div>
                        </a>
                      );
                    })}
                  </div>
                ) : null}
                {message.sources ? <Sources sources={message.sources} /> : null}
                {message.role === "assistant" ? (
                  <MessageActions onRegenerate={regenerateLast} />
                ) : null}
              </Message>
            ))
          )}

          {isLoading ? (
            <Message from="assistant">
              <MessageContent from="assistant" className="inline-flex items-center gap-3">
                <AiLoader />
                Drafting a polished response...
              </MessageContent>
            </Message>
          ) : null}
        </Conversation>

        <div className="shrink-0 border-t border-pink-300/10 bg-background/80 px-4 py-4 backdrop-blur-xl md:px-8">
          <div className="mx-auto max-w-4xl">
            <div className="mb-3 flex gap-2 overflow-x-auto pb-1">
              {suggestions.map((suggestion) => (
                <Suggestion
                  key={suggestion}
                  onClick={() => sendMessage(suggestion)}
                  disabled={isLoading}
                >
                  {suggestion}
                </Suggestion>
              ))}
            </div>
            <PromptInput
              value={input}
              attachments={attachments}
              isLoading={isLoading}
              onValueChange={setInput}
              onFilesSelected={addAttachments}
              onRemoveAttachment={removeAttachment}
              onSubmit={() => sendMessage()}
              onStop={() => {
                if (responseTimeoutRef.current) {
                  window.clearTimeout(responseTimeoutRef.current);
                  responseTimeoutRef.current = null;
                }
                setIsLoading(false);
              }}
            />
            <div className="mt-3 flex items-center justify-between px-1 text-xs text-zinc-600">
              <span className="inline-flex items-center gap-1.5">
                <CheckCircle2 className="size-3.5 text-pink-300" />
                Protected by Clerk
              </span>
              <button className="inline-flex items-center gap-1 text-zinc-500 hover:text-pink-200">
                <MoreHorizontal className="size-4" />
                More tools
              </button>
            </div>
          </div>
        </div>
      </section>
    </div>
  );
}
