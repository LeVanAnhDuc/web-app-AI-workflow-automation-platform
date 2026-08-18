"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import { executions as executionsApi } from "@/lib/api";
import type { Execution } from "@/lib/types";
import { Icons } from "@/components/ui/icons";
import { Spinner } from "@/components/ui";

interface Point {
  top: number;
  left: number;
}

/**
 * The trailing `…` menu on an execution row. Anchored with fixed coordinates
 * because the table lives inside a clipping card, so an absolutely positioned
 * popover would be cut off by the last row.
 */
export function ExecutionRowMenu({
  execution,
  onError,
}: {
  execution: Execution;
  onError: (message: string) => void;
}) {
  const [anchor, setAnchor] = useState<Point | null>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const router = useRouter();
  const queryClient = useQueryClient();

  const close = () => setAnchor(null);

  useEffect(() => {
    if (!anchor) return;
    menuRef.current?.querySelector<HTMLButtonElement>("button")?.focus();

    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        close();
        buttonRef.current?.focus();
      }
    };
    const onPointerDown = (e: PointerEvent) => {
      const target = e.target as globalThis.Node;
      if (menuRef.current?.contains(target) || buttonRef.current?.contains(target)) return;
      close();
    };

    window.addEventListener("keydown", onKey);
    window.addEventListener("pointerdown", onPointerDown, true);
    // Fixed coordinates go stale as soon as anything moves underneath.
    window.addEventListener("scroll", close, true);
    window.addEventListener("resize", close);
    return () => {
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("pointerdown", onPointerDown, true);
      window.removeEventListener("scroll", close, true);
      window.removeEventListener("resize", close);
    };
  }, [anchor]);

  const retry = useMutation({
    mutationFn: () => executionsApi.retry(execution.id),
    onSuccess: ({ execution: next }) => {
      close();
      void queryClient.invalidateQueries({ queryKey: ["executions"] });
      router.push(`/executions/${next.id}`);
    },
    onError: (err: Error) => {
      close();
      onError(err.message);
    },
  });

  const cancel = useMutation({
    mutationFn: () => executionsApi.cancel(execution.id),
    onSuccess: () => {
      close();
      void queryClient.invalidateQueries({ queryKey: ["executions"] });
    },
    onError: (err: Error) => {
      close();
      onError(err.message);
    },
  });

  const busy = retry.isPending || cancel.isPending;
  const canRetry = execution.status === "failed";
  const canCancel = execution.status === "running" || execution.status === "queued";

  return (
    <div
      className="flex justify-end"
      // The whole row navigates; nothing in this cell should trigger that.
      onClick={(e) => e.stopPropagation()}
      onKeyDown={(e) => e.stopPropagation()}
    >
      <button
        ref={buttonRef}
        type="button"
        aria-label={`Actions for execution ${execution.id}`}
        aria-haspopup="menu"
        aria-expanded={anchor !== null}
        disabled={busy}
        onClick={() => {
          if (anchor) return close();
          const rect = buttonRef.current?.getBoundingClientRect();
          if (!rect) return;
          setAnchor({ top: rect.bottom + 6, left: rect.right - 172 });
        }}
        className={clsx(
          "flex h-7 w-7 items-center justify-center rounded-lg text-ink-4",
          "hover:bg-white/8 hover:text-ink-2 focus-visible:outline-2 focus-visible:outline-accent",
          anchor && "bg-white/8 text-ink-2",
        )}
      >
        {busy ? <Spinner className="text-ink-3" /> : <Icons.dots size={16} />}
      </button>

      {anchor && (
        <div
          ref={menuRef}
          role="menu"
          aria-label="Execution actions"
          style={{ top: anchor.top, left: Math.max(8, anchor.left) }}
          className="fixed z-50 w-[172px] overflow-hidden rounded-[11px] border border-line-strong bg-raised p-1 shadow-[var(--shadow-modal)]"
        >
          <MenuItem
            icon={<Icons.expand size={14} />}
            label="Open"
            onClick={() => {
              close();
              router.push(`/executions/${execution.id}`);
            }}
          />
          {canRetry && (
            <MenuItem
              icon={<Icons.retry size={14} />}
              label="Retry"
              onClick={() => retry.mutate()}
            />
          )}
          {canCancel && (
            <MenuItem
              icon={<Icons.stop size={12} />}
              label="Cancel"
              tone="danger"
              onClick={() => cancel.mutate()}
            />
          )}
        </div>
      )}
    </div>
  );
}

function MenuItem({
  icon,
  label,
  onClick,
  tone = "default",
}: {
  icon: React.ReactNode;
  label: string;
  onClick: () => void;
  tone?: "default" | "danger";
}) {
  return (
    <button
      type="button"
      role="menuitem"
      onClick={onClick}
      className={clsx(
        "flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-left text-[12.5px]",
        "focus-visible:outline-2 focus-visible:outline-accent",
        tone === "danger"
          ? "text-danger hover:bg-danger/12"
          : "text-ink-2 hover:bg-white/6 hover:text-ink",
      )}
    >
      <span className={tone === "danger" ? "text-danger" : "text-ink-4"}>{icon}</span>
      {label}
    </button>
  );
}
