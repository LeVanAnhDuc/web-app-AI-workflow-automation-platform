"use client";

import clsx from "clsx";
import { relativeTime } from "@/lib/format";
import type { CredentialSummary, CredentialType } from "@/lib/types";
import { CredentialRowMenu } from "./CredentialRowMenu";
import { CredentialStatusPill } from "./CredentialStatusPill";
import { TypeIconChip } from "./TypeIcon";
import { credentialStatus } from "./status";

/** The one place the table's column geometry is declared, so the header, the
 *  rows and the loading skeleton cannot drift apart. */
export const ROW_GRID =
  "grid grid-cols-[2.4fr_1.5fr_112px_1fr_40px] items-center gap-3";

const columns = ["NAME", "STATUS", "USED BY", "UPDATED", ""];

export function TableHead() {
  return (
    <div
      className={clsx(
        ROW_GRID,
        "border-b border-line px-5 py-3 text-[11px] font-bold tracking-[0.06em] text-ink-5",
      )}
    >
      {columns.map((c, i) => (
        <span key={c || `col-${i}`}>{c}</span>
      ))}
    </div>
  );
}

/** Placeholder rows rather than a spinner: the table keeps its geometry, so
 *  nothing shifts when the data lands. */
export function SkeletonRows({ rows = 4 }: { rows?: number }) {
  return (
    <div aria-hidden>
      {Array.from({ length: rows }, (_, i) => (
        <div
          key={i}
          className={clsx(ROW_GRID, "border-b border-line px-5 py-[15px] last:border-b-0")}
        >
          <div className="flex items-center gap-3">
            <Block className="h-8 w-8 rounded-[9px]" />
            <div className="flex flex-col gap-1.5">
              <Block className="h-[13px] w-[148px]" />
              <Block className="h-[9px] w-[92px]" />
            </div>
          </div>
          <Block className="h-[22px] w-[96px] rounded-full" />
          <Block className="h-[11px] w-[52px]" />
          <Block className="h-[11px] w-[72px]" />
          <span />
        </div>
      ))}
    </div>
  );
}

function Block({ className }: { className?: string }) {
  return <span className={clsx("block animate-pulse rounded bg-white/6", className)} />;
}

export function CredentialRow({
  credential,
  type,
  testing,
  deleting,
  onEdit,
  onTest,
  onConnect,
  onDelete,
}: {
  credential: CredentialSummary;
  /** Undefined when this build no longer registers the type. */
  type?: CredentialType;
  testing?: boolean;
  deleting?: boolean;
  onEdit: () => void;
  onTest: () => void;
  onConnect: () => void;
  onDelete: () => void;
}) {
  const status = credentialStatus(credential, type);
  const used = credential.usedByCount;

  return (
    <div
      className={clsx(
        ROW_GRID,
        "group border-b border-line px-5 py-[15px] last:border-b-0 hover:bg-accent/6",
      )}
    >
      <div className="flex min-w-0 items-center gap-3">
        <TypeIconChip icon={type?.icon ?? "lock"} size={32} />
        <span className="flex min-w-0 flex-col gap-0.5">
          <button
            type="button"
            onClick={onEdit}
            className="truncate text-left text-[13.5px] font-semibold text-ink hover:text-accent-3 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
          >
            {credential.name}
          </button>
          <span className="truncate font-mono text-[10.5px] text-ink-4">
            {type?.name ?? credential.type}
          </span>
        </span>
      </div>

      <div className="flex min-w-0 flex-col items-start gap-1">
        <CredentialStatusPill status={status} />
        <span className="flex min-w-0 items-center gap-2">
          {status.detail && (
            <span className="truncate text-[11px] text-ink-4">{status.detail}</span>
          )}
          {/* The fix belongs next to the problem: a credential that says "Not
              connected" with no way to connect is just a complaint. */}
          {status.canConnect && (
            <button
              type="button"
              onClick={onConnect}
              className="shrink-0 rounded-md bg-accent/16 px-2 py-[2px] text-[10.5px] font-semibold text-accent-3 hover:bg-accent/26 focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-accent"
            >
              {status.connectLabel ?? "Connect"}
            </button>
          )}
        </span>
      </div>

      <span
        className={clsx("text-[12.5px]", used > 0 ? "text-ink-2" : "text-ink-5")}
        title={used > 0 ? `${used} node${used === 1 ? "" : "s"} reference it` : undefined}
      >
        {used > 0 ? `${used} node${used === 1 ? "" : "s"}` : "Unused"}
      </span>

      <span className="text-[12.5px] text-ink-3">{relativeTime(credential.updatedAt)}</span>

      <CredentialRowMenu
        name={credential.name}
        status={status}
        usedByCount={used}
        canTest={Boolean(type?.testUrl)}
        isOAuth={type?.auth === "oauth2"}
        testing={testing}
        deleting={deleting}
        onEdit={onEdit}
        onTest={onTest}
        onConnect={onConnect}
        onDelete={onDelete}
      />
    </div>
  );
}
