"use client";

import { useEffect, useState } from "react";

/**
 * A clock that re-renders its caller on an interval, but only while `active`.
 * Running executions need a duration that counts up; every other row is static,
 * so the timer is never installed when nothing on screen is live.
 */
export function useNow(active: boolean, intervalMs = 1000): Date {
  const [now, setNow] = useState(() => new Date());

  useEffect(() => {
    if (!active) return;
    // Snap immediately so switching a row to `running` does not wait a tick.
    setNow(new Date());
    const timer = setInterval(() => setNow(new Date()), intervalMs);
    return () => clearInterval(timer);
  }, [active, intervalMs]);

  return now;
}

/** Elapsed milliseconds for a run that has not finished yet, or undefined. */
export function elapsedMs(startedAt: string | undefined, now: Date): number | undefined {
  if (!startedAt) return undefined;
  const start = new Date(startedAt).getTime();
  if (Number.isNaN(start)) return undefined;
  return Math.max(0, now.getTime() - start);
}
