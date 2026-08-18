"use client";

import clsx from "clsx";
import { useCallback, useEffect, useRef, useState } from "react";
import { Icons } from "@/components/ui/icons";
import { Spinner } from "@/components/ui";

const MENU_WIDTH = 224;
const MENU_ESTIMATED_HEIGHT = 140;

/**
 * Per-row actions. The popover is `position: fixed` and placed from the
 * button's rect: the table Card clips its overflow, so an absolutely
 * positioned menu would be cut off on the last rows.
 */
export function RowMenu({
  workflowName,
  onOpen,
  onDelete,
  deleting = false,
}: {
  workflowName: string;
  onOpen: () => void;
  onDelete: () => void;
  deleting?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [pos, setPos] = useState<{ top: number; left: number } | null>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);

  const close = useCallback(() => {
    setOpen(false);
    setConfirming(false);
  }, []);

  useEffect(() => {
    if (!open) return;

    const onPointerDown = (e: PointerEvent) => {
      const target = e.target as Node;
      if (menuRef.current?.contains(target) || buttonRef.current?.contains(target)) return;
      close();
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      close();
      buttonRef.current?.focus();
    };

    document.addEventListener("pointerdown", onPointerDown, true);
    document.addEventListener("keydown", onKeyDown);
    // A fixed popover cannot follow the page, so scrolling dismisses it.
    window.addEventListener("scroll", close, true);
    window.addEventListener("resize", close);

    return () => {
      document.removeEventListener("pointerdown", onPointerDown, true);
      document.removeEventListener("keydown", onKeyDown);
      window.removeEventListener("scroll", close, true);
      window.removeEventListener("resize", close);
    };
  }, [open, close]);

  const toggle = () => {
    if (open) {
      close();
      return;
    }
    const rect = buttonRef.current?.getBoundingClientRect();
    if (rect) {
      const below = rect.bottom + 6;
      const flip = below + MENU_ESTIMATED_HEIGHT > window.innerHeight;
      setPos({
        top: flip ? Math.max(8, rect.top - 6 - MENU_ESTIMATED_HEIGHT) : below,
        left: Math.max(8, rect.right - MENU_WIDTH),
      });
    }
    setOpen(true);
  };

  return (
    // Every action here must not trigger the row's navigation.
    <div onClick={(e) => e.stopPropagation()} className="flex justify-end">
      <button
        ref={buttonRef}
        type="button"
        onClick={toggle}
        aria-label={`Actions for ${workflowName}`}
        aria-haspopup="menu"
        aria-expanded={open}
        className={clsx(
          "inline-flex h-7 w-7 items-center justify-center rounded-lg text-ink-4",
          "hover:bg-white/6 hover:text-ink-2",
          "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent",
          open && "bg-white/6 text-ink-2",
        )}
      >
        <Icons.dots size={16} />
      </button>

      {open && pos && (
        <div
          ref={menuRef}
          role="menu"
          aria-label={`Actions for ${workflowName}`}
          style={{ top: pos.top, left: pos.left, width: MENU_WIDTH }}
          className="fixed z-50 overflow-hidden rounded-[11px] border border-line-strong bg-raised p-1.5 shadow-[var(--shadow-modal)]"
        >
          {confirming ? (
            <div className="px-2 py-1.5">
              <p className="text-[12px] leading-snug text-ink-2">
                Delete “{workflowName}”? Its executions go with it.
              </p>
              <div className="mt-2.5 flex gap-2">
                <button
                  type="button"
                  onClick={() => setConfirming(false)}
                  className="grow rounded-lg px-2 py-1.5 text-[12px] font-semibold text-ink-3 hover:bg-white/6 hover:text-ink-2 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
                >
                  Cancel
                </button>
                <button
                  type="button"
                  disabled={deleting}
                  // Stays open while the request is in flight: the row itself
                  // disappears on success, and on failure the confirm view is
                  // still there next to the page's error notice.
                  onClick={onDelete}
                  className="inline-flex grow items-center justify-center gap-1.5 rounded-lg bg-danger/15 px-2 py-1.5 text-[12px] font-semibold text-danger hover:bg-danger/25 disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-danger"
                >
                  {deleting && <Spinner className="h-3 w-3" />}
                  Delete
                </button>
              </div>
            </div>
          ) : (
            <>
              <MenuItem
                icon={<Icons.expand size={14} />}
                onClick={() => {
                  close();
                  onOpen();
                }}
              >
                Open
              </MenuItem>
              <MenuItem
                icon={<Icons.copy size={14} />}
                disabled
                title="Duplicating a workflow is not in this phase"
              >
                Duplicate
              </MenuItem>
              <div className="my-1 h-px bg-line" />
              <MenuItem
                icon={<Icons.trash size={14} />}
                tone="danger"
                onClick={() => setConfirming(true)}
              >
                Delete
              </MenuItem>
            </>
          )}
        </div>
      )}
    </div>
  );
}

function MenuItem({
  icon,
  children,
  onClick,
  disabled,
  title,
  tone = "default",
}: {
  icon: React.ReactNode;
  children: React.ReactNode;
  onClick?: () => void;
  disabled?: boolean;
  title?: string;
  tone?: "default" | "danger";
}) {
  return (
    <button
      type="button"
      role="menuitem"
      onClick={onClick}
      disabled={disabled}
      title={title}
      className={clsx(
        "flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-left text-[12.5px] font-medium",
        "focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent",
        disabled && "cursor-not-allowed text-ink-5",
        !disabled && tone === "danger" && "text-danger hover:bg-danger/12",
        !disabled && tone === "default" && "text-ink-2 hover:bg-white/6 hover:text-ink",
      )}
    >
      <span className="shrink-0">{icon}</span>
      {children}
    </button>
  );
}
