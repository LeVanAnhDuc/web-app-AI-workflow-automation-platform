"use client";

import clsx from "clsx";
import { useCallback, useEffect, useRef, useState } from "react";
import { Spinner } from "@/components/ui";
import { Icons } from "@/components/ui/icons";
import { usageSentence, type CredentialStatus } from "./status";

const MENU_WIDTH = 236;
const MENU_ESTIMATED_HEIGHT = 176;

/**
 * Per-row actions. Same anatomy as the workflow row menu: the popover is
 * `position: fixed` and placed from the button's rect, because the table Card
 * clips its overflow and an absolutely positioned menu would be cut off on the
 * last rows.
 */
export function CredentialRowMenu({
  name,
  status,
  usedByCount,
  canTest,
  isOAuth,
  testing,
  deleting,
  onEdit,
  onTest,
  onConnect,
  onDelete,
}: {
  name: string;
  status: CredentialStatus;
  usedByCount: number;
  canTest: boolean;
  isOAuth: boolean;
  testing?: boolean;
  deleting?: boolean;
  onEdit: () => void;
  onTest: () => void;
  onConnect: () => void;
  onDelete: () => void;
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
    <div className="flex justify-end">
      <button
        ref={buttonRef}
        type="button"
        onClick={toggle}
        aria-label={`Actions for ${name}`}
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
          aria-label={`Actions for ${name}`}
          style={{ top: pos.top, left: pos.left, width: MENU_WIDTH }}
          className="fixed z-50 overflow-hidden rounded-[11px] border border-line-strong bg-raised p-1.5 shadow-[var(--shadow-modal)]"
        >
          {confirming ? (
            <div className="px-2 py-1.5">
              <p className="text-[12px] leading-snug text-ink-2">
                Delete “{name}”?
              </p>
              {/* The count is the whole point of the confirmation: deleting a
                  credential breaks every node pointing at it, and only the
                  server knows how many that is. */}
              <p
                className={clsx(
                  "mt-1.5 text-[11.5px] leading-snug",
                  usedByCount > 0 ? "text-warning" : "text-ink-4",
                )}
              >
                {usageSentence(usedByCount)}
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
                  onEdit();
                }}
              >
                Edit
              </MenuItem>
              <MenuItem
                icon={testing ? <Spinner className="h-3.5 w-3.5" /> : <Icons.play size={13} />}
                disabled={!canTest || testing}
                title={
                  canTest
                    ? "Call the provider with these credentials"
                    : "This credential type has no endpoint to test against"
                }
                onClick={() => {
                  close();
                  onTest();
                }}
              >
                Test connection
              </MenuItem>
              {isOAuth && (
                <MenuItem
                  icon={<Icons.retry size={14} />}
                  title="Run the provider's authorisation again"
                  onClick={() => {
                    close();
                    onConnect();
                  }}
                >
                  {status.connectLabel ?? "Reconnect"}
                </MenuItem>
              )}
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
      <span className="flex h-4 w-4 shrink-0 items-center justify-center">{icon}</span>
      {children}
    </button>
  );
}
