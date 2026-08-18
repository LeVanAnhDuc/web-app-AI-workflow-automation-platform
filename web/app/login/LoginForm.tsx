"use client";

import { useEffect, useId, useState } from "react";
import { useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError, auth } from "@/lib/api";
import { Button, Checkbox, ErrorNotice, Spinner } from "@/components/ui";
import { Icons } from "@/components/ui/icons";

function loginErrorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    // The API's 401 body is deliberately vague; the copy is ours.
    return err.status === 401 ? "Invalid email or password" : err.message;
  }
  return "Could not reach the server. Check that the API is running.";
}

export function LoginForm() {
  const router = useRouter();
  const qc = useQueryClient();
  const emailId = useId();
  const passwordId = useId();

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [reveal, setReveal] = useState(false);
  const [keepSignedIn, setKeepSignedIn] = useState(true);

  // Already signed in: the middleware catches this on a full navigation, but a
  // client-side push to /login would otherwise leave the form on screen.
  const me = useQuery({ queryKey: ["me"], queryFn: auth.me, retry: false });
  useEffect(() => {
    if (me.data) router.replace("/workflows");
  }, [me.data, router]);

  const login = useMutation({
    mutationFn: () => auth.login(email.trim(), password),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["me"] });
      router.replace("/workflows");
    },
  });

  return (
    <form
      noValidate
      onSubmit={(e) => {
        e.preventDefault();
        if (!login.isPending) login.mutate();
      }}
      className="flex flex-col gap-[18px] rounded-2xl border border-line-strong bg-panel p-7 shadow-[var(--shadow-modal)]"
    >
      <div className="flex flex-col gap-2">
        <label htmlFor={emailId} className="text-[12.5px] font-semibold text-ink-2">
          Email
        </label>
        <ControlShell icon={<Icons.mail size={15} className="text-ink-4" />}>
          <input
            id={emailId}
            type="email"
            name="email"
            autoComplete="email"
            required
            autoFocus
            placeholder="you@acme.vn"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="min-w-0 grow bg-transparent text-[13.5px] text-ink outline-none placeholder:text-ink-4"
          />
        </ControlShell>
      </div>

      <div className="flex flex-col gap-2">
        <div className="flex items-center justify-between">
          <label htmlFor={passwordId} className="text-[12.5px] font-semibold text-ink-2">
            Password
          </label>
          <button
            type="button"
            disabled
            title="Password reset arrives with the multi-tenant phase"
            className="cursor-not-allowed rounded text-[12.5px] font-semibold text-accent-2 opacity-70"
          >
            Forgot?
          </button>
        </div>
        <ControlShell icon={<Icons.lock size={15} className="text-accent-2" />}>
          <input
            id={passwordId}
            type={reveal ? "text" : "password"}
            name="password"
            autoComplete="current-password"
            required
            placeholder="••••••••••"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="min-w-0 grow bg-transparent font-mono text-[14px] tracking-[0.08em] text-ink-2 outline-none placeholder:tracking-[0.16em] placeholder:text-ink-4"
          />
          <button
            type="button"
            onClick={() => setReveal((v) => !v)}
            aria-label={reveal ? "Hide password" : "Show password"}
            aria-pressed={reveal}
            className="shrink-0 rounded p-0.5 text-ink-4 hover:text-ink-2 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent aria-pressed:text-accent-2"
          >
            <Icons.eye size={15} />
          </button>
        </ControlShell>
      </div>

      {/* The login contract is {email,password} only, so the preference has
          nowhere to go yet; the control is kept because session length is a
          server concern the API will expose later. */}
      <Checkbox
        checked={keepSignedIn}
        onChange={setKeepSignedIn}
        label="Keep me signed in for 30 days"
      />

      {login.isError && <ErrorNotice>{loginErrorMessage(login.error)}</ErrorNotice>}

      <Button
        type="submit"
        variant="primary"
        disabled={login.isPending}
        className="w-full py-[13px]! text-[14px]! font-bold!"
      >
        {login.isPending && <Spinner className="h-4 w-4" />}
        {login.isPending ? "Signing in…" : "Sign in"}
      </Button>
    </form>
  );
}

/** The bordered field frame from the mockup; the focus ring lives on the frame. */
function ControlShell({
  icon,
  children,
}: {
  icon: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div className="flex items-center gap-2.5 rounded-[10px] border border-line-strong bg-input px-3.5 py-3 focus-within:border-accent focus-within:ring-4 focus-within:ring-accent/14">
      <span className="shrink-0">{icon}</span>
      {children}
    </div>
  );
}

/** Reads the host at runtime so a self-hosted deployment names itself. */
export function SelfHostedFootnote() {
  const [host, setHost] = useState("");
  useEffect(() => setHost(window.location.host), []);

  return (
    <div className="flex items-center justify-center gap-2 text-[12.5px] text-ink-5">
      <Icons.lock size={13} />
      <span>Self-hosted workspace</span>
      <span className="text-ink-6">·</span>
      <span className="font-mono text-[11.5px]">{host || "—"}</span>
    </div>
  );
}
