"use client";

import { cn } from "@/lib/utils";

function AiLoader({ className }: { className?: string }) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 text-pink-200",
        className
      )}
    >
      <span className="size-1.5 animate-pulse rounded-full bg-pink-300" />
      <span className="size-1.5 animate-pulse rounded-full bg-fuchsia-300 [animation-delay:150ms]" />
      <span className="size-1.5 animate-pulse rounded-full bg-rose-300 [animation-delay:300ms]" />
    </span>
  );
}

export { AiLoader };
