import { type ChangeEvent, type KeyboardEvent, useCallback, useState } from "react";
import { SendHorizonal } from "lucide-react";
import { cn } from "@/lib/utils";

const MAX_INPUT_LENGTH = 2000;

interface ChatComposerProps {
  onSubmit: (text: string) => void;
  disabled?: boolean;
  placeholder?: string;
}

export function ChatComposer({ onSubmit, disabled, placeholder }: ChatComposerProps) {
  const [value, setValue] = useState("");

  const submit = useCallback(() => {
    const trimmed = value.trim();
    if (!trimmed) return;
    onSubmit(trimmed);
    setValue("");
  }, [value, onSubmit]);

  const handleChange = useCallback((e: ChangeEvent<HTMLTextAreaElement>) => {
    const next = e.target.value.slice(0, MAX_INPUT_LENGTH);
    setValue(next);
  }, []);

  const handleKeyDown = useCallback(
    (e: KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault();
        if (!disabled) submit();
      }
    },
    [submit, disabled],
  );

  const remaining = MAX_INPUT_LENGTH - value.length;
  const canSend = value.trim().length > 0 && !disabled;

  return (
    <div className="border-t border-border/50 bg-surface-1/40 px-3 pt-2 pb-3">
      <div className="relative flex items-end gap-2">
        <textarea
          aria-label="Chat message"
          value={value}
          onChange={handleChange}
          onKeyDown={handleKeyDown}
          rows={1}
          disabled={disabled}
          maxLength={MAX_INPUT_LENGTH}
          placeholder={placeholder ?? "Ask Cordum..."}
          className={cn(
            "min-h-[36px] max-h-[160px] flex-1 resize-none rounded-xl border border-border bg-surface-0 px-3 py-2 text-sm",
            "text-foreground placeholder:text-muted-foreground/60",
            "focus:border-cordum/40 focus:outline-none focus:ring-2 focus:ring-cordum/20",
            "disabled:cursor-not-allowed disabled:opacity-60",
          )}
        />
        <button
          type="button"
          onClick={submit}
          disabled={!canSend}
          aria-label="Send message"
          className={cn(
            "h-9 w-9 shrink-0 rounded-xl border transition-colors",
            "flex items-center justify-center",
            canSend
              ? "border-cordum/40 bg-cordum/10 text-cordum hover:bg-cordum/20"
              : "border-border bg-surface-1 text-muted-foreground/40",
          )}
        >
          <SendHorizonal className="h-4 w-4" />
        </button>
      </div>
      <div className="mt-1 flex items-center justify-between text-[10px] font-mono text-muted-foreground/50">
        <span>Enter to send · Shift+Enter for newline</span>
        {remaining < 200 && <span aria-live="polite">{remaining} left</span>}
      </div>
    </div>
  );
}
