import clsx from "clsx";
import { ROW_GRID } from "./grid";

const columns = ["WORKFLOW", "STATUS", "LAST RUN", "SUCCESS · 7D", "UPDATED", ""];

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

/**
 * Placeholder rows rather than a spinner: the table keeps its exact geometry,
 * so nothing shifts when the data lands.
 */
export function SkeletonRows({ rows = 5 }: { rows?: number }) {
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
          <div className="flex items-center gap-2">
            <Block className="h-[19px] w-[34px] rounded-full" />
            <Block className="h-[11px] w-[42px]" />
          </div>
          <div className="flex items-center gap-2">
            <Block className="h-[7px] w-[7px] rounded-full" />
            <Block className="h-[11px] w-[86px]" />
          </div>
          <div className="flex items-center gap-2.5">
            <Block className="h-[5px] w-[70px] rounded-full" />
            <Block className="h-[11px] w-[28px]" />
          </div>
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
