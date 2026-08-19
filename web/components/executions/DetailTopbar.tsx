"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { auth } from "@/lib/api";
import { Logo } from "@/components/layout/Topbar";
import { Icons } from "@/components/ui/icons";

/**
 * The detail screen replaces the nav tabs with a breadcrumb, exactly as the
 * editor does, so the graph replay keeps the full width.
 */
export function DetailTopbar({ executionId }: { executionId: string }) {
  const router = useRouter();
  const { data } = useQuery({ queryKey: ["me"], queryFn: auth.me, retry: false });

  const email = data?.user.email ?? "";
  const initials = email
    .split("@")[0]
    .split(/[._-]/)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase() ?? "")
    .join("");

  return (
    <header className="flex h-14 shrink-0 items-center gap-4 border-b border-line bg-panel px-[26px]">
      <Link href="/workflows" className="flex items-center gap-2.5 text-ink hover:text-ink">
        <Logo />
        <span className="text-[15px] font-bold tracking-[-0.01em]">Ducker Flow Grid</span>
      </Link>

      <span className="h-5 w-px bg-line-strong" aria-hidden />

      <nav aria-label="Breadcrumb" className="flex items-center gap-2 text-[13px] text-ink-3">
        <Link href="/executions" className="text-ink-3 hover:text-ink-2">
          Executions
        </Link>
        <Icons.chevronRight className="text-ink-6" aria-hidden />
        <span className="font-mono text-[12.5px] font-medium text-ink" aria-current="page">
          {executionId}
        </span>
      </nav>

      <div className="grow" />

      <button
        type="button"
        title={email ? `Sign out of ${email}` : "Sign out"}
        aria-label="Sign out"
        onClick={async () => {
          await auth.logout().catch(() => undefined);
          router.push("/login");
        }}
        className="flex items-center gap-[9px] rounded-full focus-visible:outline-2 focus-visible:outline-accent"
      >
        <span className="flex h-[30px] w-[30px] items-center justify-center rounded-full bg-white/10 text-xs font-bold text-ink-2">
          {initials || "?"}
        </span>
        <Icons.chevronDown className="text-ink-4" aria-hidden />
      </button>
    </header>
  );
}
