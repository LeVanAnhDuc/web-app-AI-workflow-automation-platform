"use client";

import clsx from "clsx";
import type React from "react";
import { useEffect } from "react";
import type { Status } from "@/lib/types";

/* ---------------------------------------------------------------------------
   The whole primitive set, hand-written against the Studio dark tokens. One
   file so feature code has a single import and there is one place to change a
   control's anatomy.
   --------------------------------------------------------------------------- */

type ButtonVariant = "primary" | "secondary" | "ghost" | "danger" | "accentSoft";

export function Button({
  variant = "secondary",
  className,
  children,
  ...rest
}: React.ButtonHTMLAttributes<HTMLButtonElement> & { variant?: ButtonVariant }) {
  return (
    <button
      {...rest}
      className={clsx(
        "inline-flex items-center justify-center gap-2 rounded-[10px] px-4 py-2.5 text-[13px] font-semibold",
        "transition-colors disabled:cursor-not-allowed disabled:opacity-45",
        variant === "primary" &&
          "bg-accent text-white shadow-[var(--shadow-accent)] hover:bg-accent-deep",
        variant === "secondary" &&
          "border border-line-strong bg-white/5 text-ink-2 hover:bg-white/10",
        variant === "ghost" && "text-ink-3 hover:bg-white/5 hover:text-ink",
        variant === "accentSoft" &&
          "bg-accent/14 text-accent-3 hover:bg-accent/22",
        variant === "danger" && "bg-danger/15 text-danger hover:bg-danger/25",
        className,
      )}
    >
      {children}
    </button>
  );
}

export function IconButton({
  className,
  children,
  ...rest
}: React.ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      {...rest}
      className={clsx(
        "inline-flex h-8 w-8 items-center justify-center rounded-lg text-ink-4",
        "hover:bg-white/5 hover:text-ink-2",
        className,
      )}
    >
      {children}
    </button>
  );
}

export function Field({
  label,
  hint,
  action,
  children,
}: {
  label?: string;
  hint?: React.ReactNode;
  action?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-[7px]">
      {(label || action) && (
        <div className="flex items-center justify-between">
          {label && (
            <span className="text-xs font-semibold text-ink-3">{label}</span>
          )}
          {action}
        </div>
      )}
      {children}
      {hint && <div className="font-mono text-[10.5px] text-ink-5">{hint}</div>}
    </div>
  );
}

const controlClass =
  "w-full rounded-[10px] border border-line bg-input px-3.5 py-3 text-[13px] text-ink outline-none " +
  "placeholder:text-ink-4 focus:border-accent focus:ring-4 focus:ring-accent/14";

export function Input({
  className,
  ...rest
}: React.InputHTMLAttributes<HTMLInputElement>) {
  return <input {...rest} className={clsx(controlClass, className)} />;
}

export function Textarea({
  className,
  ...rest
}: React.TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return (
    <textarea
      {...rest}
      className={clsx(controlClass, "resize-y font-mono text-xs leading-relaxed", className)}
    />
  );
}

export function Select({
  className,
  children,
  ...rest
}: React.SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <div className="relative">
      <select
        {...rest}
        className={clsx(
          controlClass,
          "appearance-none pr-9",
          className,
        )}
      >
        {children}
      </select>
      <svg
        className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-ink-4"
        width="12"
        height="12"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
      >
        <path d="M6 9l6 6 6-6" />
      </svg>
    </div>
  );
}

export function Toggle({
  checked,
  onChange,
  tone = "accent",
  label,
  disabled,
}: {
  checked: boolean;
  onChange: (next: boolean) => void;
  tone?: "accent" | "success";
  label?: string;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={clsx(
        "flex h-[19px] w-[34px] shrink-0 items-center rounded-full px-[3px] transition-colors",
        disabled && "cursor-not-allowed opacity-50",
        checked
          ? tone === "success"
            ? "justify-end bg-success"
            : "justify-end bg-accent"
          : "justify-start bg-[#232838]",
      )}
    >
      <span
        className={clsx(
          "h-[13px] w-[13px] rounded-full",
          checked ? (tone === "success" ? "bg-surface" : "bg-white") : "bg-ink-5",
        )}
      />
    </button>
  );
}

export function Checkbox({
  checked,
  onChange,
  label,
}: {
  checked: boolean;
  onChange: (next: boolean) => void;
  label?: React.ReactNode;
}) {
  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={checked}
      onClick={() => onChange(!checked)}
      className="flex items-center gap-2.5 text-left"
    >
      <span
        className={clsx(
          "flex h-[17px] w-[17px] items-center justify-center rounded-[5px] border",
          checked ? "border-accent bg-accent" : "border-line-strong bg-input",
        )}
      >
        {checked && (
          <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="#fff" strokeWidth="3" strokeLinecap="round">
            <path d="M5 13l4 4L19 7" />
          </svg>
        )}
      </span>
      {label && <span className="text-[13px] text-ink-2">{label}</span>}
    </button>
  );
}

export function Segmented<T extends string>({
  value,
  options,
  onChange,
  size = "md",
}: {
  value: T;
  options: { value: T; label: string }[];
  onChange: (next: T) => void;
  size?: "sm" | "md";
}) {
  return (
    <div
      className={clsx(
        "flex rounded-[10px] border border-line bg-panel",
        size === "md" ? "p-[3px]" : "p-[2px]",
      )}
    >
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          onClick={() => onChange(o.value)}
          className={clsx(
            "rounded-[7px] font-medium",
            size === "md" ? "px-[15px] py-[7px] text-[12.5px]" : "px-[11px] py-1 text-[11.5px]",
            o.value === value
              ? "bg-white/8 font-semibold text-ink"
              : "text-ink-3 hover:text-ink-2",
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

export function Card({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <div
      className={clsx(
        "overflow-hidden rounded-[14px] border border-line bg-panel",
        className,
      )}
    >
      {children}
    </div>
  );
}

const statusTone: Record<Status, { label: string; text: string; bg: string; dot: string }> = {
  queued: { label: "Queued", text: "text-ink-3", bg: "bg-white/6", dot: "bg-ink-5" },
  running: { label: "Running", text: "text-accent-3", bg: "bg-accent/15", dot: "bg-accent-2" },
  succeeded: { label: "Succeeded", text: "text-success", bg: "bg-success/13", dot: "bg-success" },
  failed: { label: "Failed", text: "text-danger", bg: "bg-danger/15", dot: "bg-danger" },
  skipped: { label: "Skipped", text: "text-ink-4", bg: "bg-white/5", dot: "bg-ink-6" },
  cancelled: { label: "Cancelled", text: "text-warning", bg: "bg-warning/13", dot: "bg-warning" },
};

export function StatusPill({ status, className }: { status: Status; className?: string }) {
  const tone = statusTone[status] ?? statusTone.queued;
  return (
    <span
      className={clsx(
        "inline-flex w-fit items-center gap-[7px] rounded-full px-2.5 py-1",
        tone.bg,
        className,
      )}
    >
      <span className={clsx("h-1.5 w-1.5 rounded-full", tone.dot)} />
      <span className={clsx("text-[11.5px] font-bold", tone.text)}>{tone.label}</span>
    </span>
  );
}

export function StatusDot({ status }: { status: Status }) {
  const tone = statusTone[status] ?? statusTone.queued;
  return <span className={clsx("inline-block h-[7px] w-[7px] shrink-0 rounded-full", tone.dot)} />;
}

export function Badge({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <span
      className={clsx(
        "rounded-full bg-white/6 px-[7px] py-0.5 text-[11px] text-ink-3",
        className,
      )}
    >
      {children}
    </span>
  );
}

export function Kbd({ children }: { children: React.ReactNode }) {
  return (
    <span className="rounded-[5px] bg-white/6 px-[7px] py-[3px] font-mono text-[10px] text-ink-3">
      {children}
    </span>
  );
}

export function Modal({
  open,
  onClose,
  children,
  width = 680,
  labelledBy,
}: {
  open: boolean;
  onClose: () => void;
  children: React.ReactNode;
  width?: number;
  labelledBy?: string;
}) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  if (!open) return null;
  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center">
      <div
        className="absolute inset-0 bg-[rgba(8,9,13,0.78)]"
        onClick={onClose}
        aria-hidden
      />
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby={labelledBy}
        style={{ width }}
        className="relative mt-[168px] max-h-[70vh] overflow-hidden rounded-2xl border border-line-strong bg-[#14171f] shadow-[var(--shadow-modal)]"
      >
        {children}
      </div>
    </div>
  );
}

export function Spinner({ className }: { className?: string }) {
  return (
    <svg
      className={clsx("animate-spin", className)}
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
    >
      <circle cx="12" cy="12" r="9" stroke="currentColor" strokeOpacity="0.2" strokeWidth="3" />
      <path d="M21 12a9 9 0 0 0-9-9" stroke="currentColor" strokeWidth="3" strokeLinecap="round" />
    </svg>
  );
}

export function EmptyState({
  title,
  description,
  action,
}: {
  title: string;
  description?: string;
  action?: React.ReactNode;
}) {
  return (
    <div className="flex flex-col items-center gap-3 px-6 py-16 text-center">
      <div className="text-[15px] font-semibold text-ink-2">{title}</div>
      {description && (
        <div className="max-w-sm text-[13px] leading-relaxed text-ink-4">{description}</div>
      )}
      {action && <div className="mt-2">{action}</div>}
    </div>
  );
}

export function ErrorNotice({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex items-start gap-2.5 rounded-[10px] border border-danger/25 bg-danger/8 px-3.5 py-3">
      <svg
        className="mt-px shrink-0 text-danger"
        width="15"
        height="15"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
      >
        <circle cx="12" cy="12" r="9" />
        <path d="M12 8v5M12 16.5v.01" />
      </svg>
      <span className="text-xs leading-snug text-danger-3">{children}</span>
    </div>
  );
}

export function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="text-[11px] font-bold tracking-[0.07em] text-ink-5">{children}</div>
  );
}

/** Renders a JSON value with the palette's syntax colours. */
export function JsonView({ value, className }: { value: unknown; className?: string }) {
  return (
    <pre
      className={clsx(
        "overflow-auto rounded-[11px] border border-line bg-raised-2 p-4 font-mono text-[11.5px] leading-[1.7]",
        className,
      )}
    >
      {tokenize(value, 0)}
    </pre>
  );
}

function tokenize(value: unknown, depth: number): React.ReactNode {
  const pad = "  ".repeat(depth);
  const padIn = "  ".repeat(depth + 1);

  if (value === null) return <span className="text-json-punct">null</span>;
  if (typeof value === "number" || typeof value === "boolean")
    return <span className="text-json-number">{String(value)}</span>;
  if (typeof value === "string")
    return <span className="text-json-string">{JSON.stringify(value)}</span>;

  if (Array.isArray(value)) {
    if (value.length === 0) return <span className="text-json-punct">[]</span>;
    return (
      <>
        <span className="text-json-punct">[</span>
        {value.map((v, i) => (
          <span key={i}>
            {"\n"}
            {padIn}
            {tokenize(v, depth + 1)}
            {i < value.length - 1 && <span className="text-json-punct">,</span>}
          </span>
        ))}
        {"\n"}
        {pad}
        <span className="text-json-punct">]</span>
      </>
    );
  }

  const entries = Object.entries(value as Record<string, unknown>);
  if (entries.length === 0) return <span className="text-json-punct">{"{}"}</span>;
  return (
    <>
      <span className="text-json-punct">{"{"}</span>
      {entries.map(([k, v], i) => (
        <span key={k}>
          {"\n"}
          {padIn}
          <span className="text-json-key">{JSON.stringify(k)}</span>
          <span className="text-json-punct">: </span>
          {tokenize(v, depth + 1)}
          {i < entries.length - 1 && <span className="text-json-punct">,</span>}
        </span>
      ))}
      {"\n"}
      {pad}
      <span className="text-json-punct">{"}"}</span>
    </>
  );
}
