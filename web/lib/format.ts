/** Human-facing formatting shared by every screen. */

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/** "2 minutes ago", "6 hours ago", "yesterday", "2 weeks ago". */
export function relativeTime(iso: string | undefined, now: Date = new Date()): string {
  if (!iso) return "—";
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return "—";
  const diff = now.getTime() - then;

  if (diff < 5_000) return "just now";
  if (diff < MINUTE) return `${Math.floor(diff / 1000)} seconds ago`;
  if (diff < HOUR) {
    const m = Math.floor(diff / MINUTE);
    return `${m} minute${m === 1 ? "" : "s"} ago`;
  }
  if (diff < DAY) {
    const h = Math.floor(diff / HOUR);
    return `${h} hour${h === 1 ? "" : "s"} ago`;
  }
  const d = Math.floor(diff / DAY);
  if (d === 1) return "yesterday";
  if (d < 7) return `${d} days ago`;
  const w = Math.floor(d / 7);
  if (w < 5) return `${w} week${w === 1 ? "" : "s"} ago`;
  const mo = Math.floor(d / 30);
  return `${mo} month${mo === 1 ? "" : "s"} ago`;
}

/** "2ms", "318ms", "1.42s", "12.8s", "2m 04s". */
export function duration(ms: number | undefined | null): string {
  if (ms === undefined || ms === null) return "—";
  if (ms < 1000) return `${Math.round(ms)}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(2).replace(/0$/, "")}s`;
  const m = Math.floor(ms / 60_000);
  const s = Math.round((ms % 60_000) / 1000);
  return `${m}m ${String(s).padStart(2, "0")}s`;
}

/** Clock time with tenths, as the execution log shows it: "14:02:11.4". */
export function clockTime(iso: string | undefined): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  const p = (n: number, w = 2) => String(n).padStart(w, "0");
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${Math.floor(d.getMilliseconds() / 100)}`;
}

/** "18 Aug 2026, 14:31:01" for the execution header. */
export function absoluteTime(iso: string | undefined): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleString("en-GB", {
    day: "numeric",
    month: "short",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

/** 0.98 -> "98%" */
export function percent(rate: number | undefined | null): string {
  if (rate === undefined || rate === null) return "—";
  return `${Math.round(rate * 100)}%`;
}

/** Trigger label as the tables show it. */
export function triggerLabel(type: string, detail?: string): string {
  const base =
    type === "webhook" ? "Webhook" : type === "schedule" ? "Schedule" : "Manual";
  return detail ? `${base} ${detail}` : base;
}

/** One-line failure reason for a table subline. */
export function errorSubline(err?: { code: string; message: string; node_name?: string }): string {
  if (!err) return "";
  const where = err.node_name ? ` at “${err.node_name}”` : "";
  return `${err.message}${where}`;
}

/** Truncates in the middle so both ends of a URL stay readable. */
export function middleTruncate(value: string, max = 64): string {
  if (value.length <= max) return value;
  const half = Math.floor((max - 1) / 2);
  return `${value.slice(0, half)}…${value.slice(-half)}`;
}
