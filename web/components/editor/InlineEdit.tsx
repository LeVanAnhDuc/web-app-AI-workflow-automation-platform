"use client";

import clsx from "clsx";
import { useEffect, useRef, useState } from "react";

/**
 * Click-to-edit text, used for both the workflow name in the breadcrumb and the
 * node name in the drawer. Enter or blur commits, Escape reverts — the
 * behaviour people expect from a renameable label, and the reason neither call
 * site needs a form or a modal.
 */
export function InlineEdit({
  value,
  onSave,
  ariaLabel,
  className,
  inputClassName,
}: {
  value: string;
  onSave: (next: string) => void;
  ariaLabel: string;
  className?: string;
  inputClassName?: string;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(value);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (editing) {
      inputRef.current?.focus();
      inputRef.current?.select();
    }
  }, [editing]);

  // A rename that lands from elsewhere (uniqueness suffix, another tab) must not
  // be overwritten by a stale draft.
  useEffect(() => {
    if (!editing) setDraft(value);
  }, [value, editing]);

  const commit = () => {
    setEditing(false);
    const next = draft.trim();
    if (next && next !== value) onSave(next);
    else setDraft(value);
  };

  if (!editing) {
    return (
      <button
        type="button"
        onClick={() => setEditing(true)}
        aria-label={`${ariaLabel}: ${value}. Click to rename`}
        className={clsx(
          "max-w-full truncate rounded-[6px] px-1 text-left -mx-1 hover:bg-white/6",
          className,
        )}
      >
        {value}
      </button>
    );
  }

  return (
    <input
      ref={inputRef}
      value={draft}
      aria-label={ariaLabel}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === "Enter") {
          e.preventDefault();
          commit();
        } else if (e.key === "Escape") {
          e.preventDefault();
          setDraft(value);
          setEditing(false);
        }
      }}
      className={clsx(
        "min-w-0 rounded-[6px] border border-accent bg-input px-1 outline-none -mx-1",
        className,
        inputClassName,
      )}
    />
  );
}
