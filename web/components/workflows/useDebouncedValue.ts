"use client";

import { useEffect, useState } from "react";

/**
 * Trails `value` by `delay`. Used for the name filter so typing does not put
 * one request per keystroke on the wire (and one query cache entry per prefix).
 */
export function useDebouncedValue<T>(value: T, delay = 250): T {
  const [debounced, setDebounced] = useState(value);

  useEffect(() => {
    const t = setTimeout(() => setDebounced(value), delay);
    return () => clearTimeout(t);
  }, [value, delay]);

  return debounced;
}
