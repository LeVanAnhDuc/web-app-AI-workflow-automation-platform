"use client";

import Link from "next/link";
import { Badge, Button, IconButton, Spinner, Toggle } from "@/components/ui";
import { Icons } from "@/components/ui/icons";
import { Logo } from "@/components/layout/Topbar";
import { InlineEdit } from "./InlineEdit";

/**
 * The editor's own topbar. It deliberately does not reuse `<Topbar>`: here the
 * breadcrumb takes the place of the nav tabs so the canvas keeps the full width,
 * which was an explicit decision in the approved design.
 */
export function EditorTopbar({
  name,
  version,
  active,
  dirty,
  saving,
  running,
  starting,
  problems,
  onRename,
  onToggleActive,
  onSave,
  onDismissProblems,
  onRun,
  onStop,
}: {
  name: string;
  version: number;
  active: boolean;
  dirty: boolean;
  saving: boolean;
  running: boolean;
  starting: boolean;
  problems: string[];
  onRename: (next: string) => void;
  onToggleActive: (next: boolean) => void;
  onSave: () => void;
  onDismissProblems: () => void;
  onRun: () => void;
  onStop: () => void;
}) {
  return (
    <header className="flex h-14 shrink-0 items-center gap-4 border-b border-line bg-panel px-4.5">
      <Link href="/workflows" className="flex items-center gap-2.5 text-ink hover:text-ink">
        <Logo />
        <span className="text-[15px] font-bold tracking-[-0.01em]">Ducker Flow Grid</span>
      </Link>

      <span className="h-5 w-px bg-line-strong" aria-hidden />

      <nav aria-label="Breadcrumb" className="flex min-w-0 items-center gap-2 text-[13px]">
        <Link href="/workflows" className="text-ink-3 hover:text-ink-2">
          Workflows
        </Link>
        <Icons.chevronRight size={12} className="shrink-0 text-ink-5" />
        <InlineEdit
          value={name}
          ariaLabel="Workflow name"
          onSave={onRename}
          className="font-semibold text-ink"
          inputClassName="w-52 font-semibold"
        />
        <Badge className="shrink-0">v{version}</Badge>
      </nav>

      <div className="grow" />

      <div className="flex items-center gap-2.5 text-[12.5px] text-ink-3">
        <span>{active ? "Active" : "Inactive"}</span>
        <Toggle
          checked={active}
          onChange={onToggleActive}
          tone="success"
          label="Workflow active"
        />
      </div>

      <div className="relative">
        <Button variant="secondary" onClick={onSave} disabled={!dirty || saving}>
          {saving ? <Spinner className="h-4 w-4" /> : null}
          {dirty ? "Save" : "Saved"}
        </Button>

        {problems.length > 0 && (
          <div
            role="alert"
            className="absolute right-0 top-[calc(100%+8px)] z-40 w-90 rounded-[12px] border border-danger/25 bg-raised p-3.5 shadow-[var(--shadow-modal)]"
          >
            <div className="mb-2 flex items-center gap-2">
              <Icons.alert size={14} className="text-danger" />
              <span className="text-xs font-bold text-danger-3">
                {problems.length === 1 ? "One problem" : `${problems.length} problems`} to fix first
              </span>
              <div className="grow" />
              <IconButton aria-label="Dismiss the validation problems" onClick={onDismissProblems}>
                <Icons.close size={13} />
              </IconButton>
            </div>
            <ul className="flex flex-col gap-1.5">
              {problems.map((p) => (
                <li key={p} className="text-[12px] leading-snug text-ink-2">
                  {p}
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>

      {running ? (
        <Button variant="danger" onClick={onStop}>
          <Icons.stop size={11} />
          Stop
        </Button>
      ) : (
        <Button variant="primary" onClick={onRun} disabled={starting}>
          {starting ? <Spinner className="h-4 w-4" /> : <Icons.play size={11} />}
          Run
        </Button>
      )}
    </header>
  );
}
