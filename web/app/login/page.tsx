import type { Metadata } from "next";
import { Logo } from "@/components/layout/Topbar";
import { LoginForm, SelfHostedFootnote } from "./LoginForm";

export const metadata: Metadata = {
  title: "Sign in · Flowgrid",
};

/**
 * The backdrop is static markup, so it stays on the server; only the card and
 * the host footnote are client components.
 */
export default function LoginPage() {
  return (
    <main className="relative flex min-h-screen items-center justify-center overflow-hidden bg-surface px-6 py-16">
      <div aria-hidden className="canvas-dots absolute inset-0" />

      {/* The violet glow: the token colour carries no alpha, so the layer's own
          opacity produces the wash instead of a hardcoded rgba. */}
      <div
        aria-hidden
        className="absolute left-1/2 top-[-180px] h-[560px] w-[560px] -translate-x-[280px] rounded-full bg-[radial-gradient(circle,var(--color-accent),transparent_66%)] opacity-20"
      />

      <div
        aria-hidden
        className="absolute inset-x-0 bottom-0 h-[300px] bg-linear-to-t from-surface to-transparent"
      />

      <DecorativeNodes />

      <div className="relative flex w-[404px] max-w-full flex-col gap-[26px]">
        <div className="flex flex-col items-center gap-4">
          <div className="rounded-[13px] shadow-[var(--shadow-accent)]">
            <Logo size={46} />
          </div>
          <div className="flex flex-col items-center gap-[7px]">
            <h1 className="text-2xl font-extrabold tracking-[-0.025em]">Sign in to Flowgrid</h1>
            <p className="text-[13.5px] text-ink-3">
              Enter your workspace credentials to continue.
            </p>
          </div>
        </div>

        <LoginForm />
        <SelfHostedFootnote />
      </div>
    </main>
  );
}

/**
 * The two faint node cards from the mockup. `slice` keeps the 1440x900
 * composition's proportions on any viewport instead of stretching it.
 */
function DecorativeNodes() {
  return (
    <svg
      aria-hidden
      className="pointer-events-none absolute inset-0 h-full w-full"
      viewBox="0 0 1440 900"
      preserveAspectRatio="xMidYMid slice"
    >
      <path
        d="M232 300 H360"
        className="stroke-accent/25"
        strokeWidth={2}
        strokeLinecap="round"
        fill="none"
      />
      <path
        d="M1080 620 H1208"
        className="stroke-accent/20"
        strokeWidth={2}
        strokeLinecap="round"
        fill="none"
      />
      {[
        { x: 140, y: 282 },
        { x: 360, y: 282 },
        { x: 988, y: 602 },
        { x: 1208, y: 602 },
      ].map((r) => (
        <rect
          key={`${r.x}-${r.y}`}
          x={r.x}
          y={r.y}
          width={92}
          height={36}
          rx={9}
          className="fill-panel stroke-line"
        />
      ))}
      <circle cx={232} cy={300} r={4} className="fill-accent" />
      <circle cx={1080} cy={620} r={4} className="fill-accent" />
    </svg>
  );
}
