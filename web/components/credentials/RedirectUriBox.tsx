"use client";

import { useEffect, useState } from "react";
import { Icons } from "@/components/ui/icons";

/**
 * The redirect URL, shown wherever an OAuth credential is configured.
 *
 * It is here rather than in a help page because a mismatch between this string
 * and the one registered with the provider is the single most common way the
 * dance fails, and the provider's error page never says which string it wanted.
 */
export function RedirectUriBox({ uri }: { uri: string }) {
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!copied) return;
    const t = setTimeout(() => setCopied(false), 1600);
    return () => clearTimeout(t);
  }, [copied]);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(uri);
      setCopied(true);
    } catch {
      // Clipboard access can be denied (an insecure origin, a locked-down
      // browser). The value is selectable text, so the user can still copy it
      // by hand — better than an error that suggests something is broken.
      setCopied(false);
    }
  };

  return (
    <div className="flex flex-col gap-2 rounded-[10px] border border-accent/25 bg-accent/8 px-3.5 py-3">
      <div className="flex items-center gap-2">
        <Icons.info size={13} className="shrink-0 text-accent-2" />
        <span className="text-[11px] font-bold tracking-[0.06em] text-accent-3">
          REDIRECT URL
        </span>
      </div>
      <p className="text-[11.5px] leading-relaxed text-ink-3">
        Register this exact URL with the provider. If it differs by even a trailing
        slash the provider refuses the authorisation.
      </p>
      <div className="flex items-center gap-2">
        <code className="min-w-0 grow truncate rounded-lg bg-black/25 px-2.5 py-2 font-mono text-[11px] text-ink-2 select-all">
          {uri || "—"}
        </code>
        <button
          type="button"
          onClick={copy}
          disabled={!uri}
          aria-label="Copy the redirect URL"
          className="inline-flex shrink-0 items-center gap-1.5 rounded-lg bg-white/8 px-2.5 py-2 text-[11.5px] font-semibold text-ink-2 hover:bg-white/14 disabled:opacity-45 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        >
          <Icons.copy size={13} />
          {copied ? "Copied" : "Copy"}
        </button>
      </div>
    </div>
  );
}
