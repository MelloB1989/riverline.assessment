"use client";

import * as React from "react";
import {
  Check,
  Copy,
  RefreshCw,
  Sparkles,
  ThumbsDown,
  ThumbsUp,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

type MessageProps = React.ComponentProps<"article"> & {
  from: "user" | "assistant";
};

function Message({ from, className, children, ...props }: MessageProps) {
  const isUser = from === "user";

  return (
    <article
      data-role={from}
      className={cn(
        "group flex w-full gap-3",
        isUser ? "justify-end" : "justify-start",
        className
      )}
      {...props}
    >
      {!isUser ? (
        <div className="mt-1 flex size-9 shrink-0 items-center justify-center rounded-full border border-pink-300/25 bg-pink-400/10 text-pink-200 shadow-[0_0_24px_rgba(236,72,153,0.25)]">
          <Sparkles className="size-4" />
        </div>
      ) : null}
      <div
        className={cn(
          "flex max-w-[min(720px,84%)] flex-col gap-2",
          isUser && "items-end"
        )}
      >
        {children}
      </div>
    </article>
  );
}

function MessageContent({
  from,
  className,
  children,
}: React.ComponentProps<"div"> & {
  from: "user" | "assistant";
}) {
  return (
    <div
      className={cn(
        "rounded-2xl px-4 py-3 text-sm leading-6 shadow-2xl",
        from === "user"
          ? "bg-gradient-to-br from-pink-500 via-fuchsia-500 to-rose-500 text-white shadow-pink-950/30"
          : "border border-white/10 bg-zinc-950/70 text-zinc-100 shadow-black/30 backdrop-blur-xl",
        className
      )}
    >
      {children}
    </div>
  );
}

function MessageActions({
  className,
  onRegenerate,
}: {
  className?: string;
  onRegenerate?: () => void;
}) {
  const [copied, setCopied] = React.useState(false);

  return (
    <div
      className={cn(
        "flex items-center gap-1 opacity-0 transition-opacity group-hover:opacity-100",
        className
      )}
    >
      <Button
        type="button"
        size="icon"
        variant="ghost"
        className="size-8 text-zinc-400 hover:bg-pink-400/10 hover:text-pink-100"
        onClick={() => {
          setCopied(true);
          window.setTimeout(() => setCopied(false), 1200);
        }}
      >
        {copied ? <Check className="size-4" /> : <Copy className="size-4" />}
        <span className="sr-only">Copy message</span>
      </Button>
      <Button
        type="button"
        size="icon"
        variant="ghost"
        className="size-8 text-zinc-400 hover:bg-pink-400/10 hover:text-pink-100"
      >
        <ThumbsUp className="size-4" />
        <span className="sr-only">Like response</span>
      </Button>
      <Button
        type="button"
        size="icon"
        variant="ghost"
        className="size-8 text-zinc-400 hover:bg-pink-400/10 hover:text-pink-100"
      >
        <ThumbsDown className="size-4" />
        <span className="sr-only">Dislike response</span>
      </Button>
      {onRegenerate ? (
        <Button
          type="button"
          size="icon"
          variant="ghost"
          className="size-8 text-zinc-400 hover:bg-pink-400/10 hover:text-pink-100"
          onClick={onRegenerate}
        >
          <RefreshCw className="size-4" />
          <span className="sr-only">Regenerate</span>
        </Button>
      ) : null}
    </div>
  );
}

export { Message, MessageActions, MessageContent };
