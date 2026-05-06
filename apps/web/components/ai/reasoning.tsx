"use client";

import * as React from "react";
import { ChevronDown, Sparkles } from "lucide-react";

import { cn } from "@/lib/utils";

function Reasoning({
  title = "Reasoning",
  children,
  className,
}: React.ComponentProps<"details"> & {
  title?: string;
}) {
  return (
    <details
      className={cn(
        "group rounded-2xl border border-pink-300/15 bg-pink-400/5 px-4 py-3 text-sm text-zinc-300",
        className
      )}
    >
      <summary className="flex cursor-pointer list-none items-center justify-between gap-3 text-pink-100">
        <span className="inline-flex items-center gap-2">
          <Sparkles className="size-4 text-pink-300" />
          {title}
        </span>
        <ChevronDown className="size-4 transition-transform group-open:rotate-180" />
      </summary>
      <div className="mt-3 leading-6 text-zinc-400">{children}</div>
    </details>
  );
}

export { Reasoning };
