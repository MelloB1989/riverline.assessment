"use client";

import * as React from "react";
import {
  ArrowUp,
  FileText,
  ImageIcon,
  Paperclip,
  SlidersHorizontal,
  StopCircle,
  X,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

export type PromptAttachment = {
  id: string;
  name: string;
  size: number;
  type: string;
  url?: string;
};

type PromptInputProps = Omit<React.ComponentProps<"form">, "onSubmit"> & {
  value: string;
  isLoading?: boolean;
  attachments?: PromptAttachment[];
  placeholder?: string;
  onValueChange: (value: string) => void;
  onFilesSelected?: (files: File[]) => void;
  onRemoveAttachment?: (id: string) => void;
  onSubmit: () => void;
  onStop?: () => void;
};

function formatFileSize(size: number) {
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / 1024 / 1024).toFixed(1)} MB`;
}

function PromptInput({
  value,
  isLoading,
  attachments = [],
  placeholder = "Ask Riverline anything...",
  className,
  onValueChange,
  onFilesSelected,
  onRemoveAttachment,
  onSubmit,
  onStop,
  ...props
}: PromptInputProps) {
  const textareaRef = React.useRef<HTMLTextAreaElement>(null);
  const fileInputRef = React.useRef<HTMLInputElement>(null);

  React.useEffect(() => {
    const textarea = textareaRef.current;
    if (!textarea) return;

    textarea.style.height = "0px";
    textarea.style.height = `${Math.min(textarea.scrollHeight, 180)}px`;
  }, [value]);

  return (
    <form
      className={cn(
        "rounded-3xl border border-pink-300/20 bg-zinc-950/80 p-2 shadow-[0_0_60px_rgba(236,72,153,0.18)] backdrop-blur-2xl",
        className
      )}
      onSubmit={(event) => {
        event.preventDefault();
        onSubmit();
      }}
      {...props}
    >
      {attachments.length > 0 ? (
        <div className="grid gap-2 p-2 sm:grid-cols-2">
          {attachments.map((attachment) => {
            const isImage = attachment.type.startsWith("image/");

            return (
              <div
                key={attachment.id}
                className="group flex min-w-0 items-center gap-3 rounded-2xl border border-pink-300/15 bg-white/[0.04] p-2 pr-3 shadow-inner shadow-white/5"
              >
                <div className="grid size-11 shrink-0 place-items-center overflow-hidden rounded-xl bg-pink-400/10 text-pink-200">
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
                    {attachment.type || "File"} · {formatFileSize(attachment.size)}
                  </p>
                </div>
                <button
                  type="button"
                  className="grid size-7 shrink-0 place-items-center rounded-full text-zinc-500 transition hover:bg-pink-400/15 hover:text-pink-100"
                  onClick={() => onRemoveAttachment?.(attachment.id)}
                >
                  <X className="size-4" />
                  <span className="sr-only">Remove {attachment.name}</span>
                </button>
              </div>
            );
          })}
        </div>
      ) : null}
      <textarea
        ref={textareaRef}
        value={value}
        onChange={(event) => onValueChange(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === "Enter" && !event.shiftKey) {
            event.preventDefault();
            onSubmit();
          }
        }}
        placeholder={placeholder}
        rows={1}
        className="min-h-14 w-full resize-none bg-transparent px-4 py-3 text-[15px] leading-6 text-zinc-50 outline-none placeholder:text-zinc-500"
      />
      <input
        ref={fileInputRef}
        type="file"
        multiple
        className="hidden"
        onChange={(event) => {
          const files = Array.from(event.target.files ?? []);
          if (files.length > 0) {
            onFilesSelected?.(files);
          }
          event.target.value = "";
        }}
      />
      <div className="flex items-center justify-between gap-3 px-2 pb-1">
        <div className="flex items-center gap-1">
          <Button
            type="button"
            size="icon"
            variant="ghost"
            className="size-9 rounded-full text-zinc-400 hover:bg-pink-400/10 hover:text-pink-100"
            onClick={() => fileInputRef.current?.click()}
          >
            <Paperclip className="size-4" />
            <span className="sr-only">Attach files</span>
          </Button>
          <Button
            type="button"
            size="sm"
            variant="ghost"
            className="h-9 rounded-full px-3 text-zinc-400 hover:bg-pink-400/10 hover:text-pink-100"
          >
            <SlidersHorizontal className="size-4" />
            Deep context
          </Button>
        </div>
        {isLoading ? (
          <Button
            type="button"
            size="icon"
            className="size-9 rounded-full bg-zinc-100 text-zinc-950 hover:bg-white"
            onClick={onStop}
          >
            <StopCircle className="size-4" />
            <span className="sr-only">Stop response</span>
          </Button>
        ) : (
          <Button
            type="submit"
            size="icon"
            className="size-9 rounded-full bg-pink-500 text-white shadow-[0_0_24px_rgba(236,72,153,0.45)] hover:bg-pink-400"
            disabled={!value.trim()}
          >
            <ArrowUp className="size-4" />
            <span className="sr-only">Send message</span>
          </Button>
        )}
      </div>
    </form>
  );
}

export { PromptInput, formatFileSize };
