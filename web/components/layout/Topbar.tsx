"use client";

import clsx from "clsx";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { auth } from "@/lib/api";
import { Icons } from "@/components/ui/icons";
import { Kbd } from "@/components/ui";

/** The product mark, reused by the topbar and the login screen. */
export function Logo({ size = 28 }: { size?: number }) {
  return (
    <div
      className="flex items-center justify-center rounded-lg bg-linear-145 from-accent to-accent-deep"
      style={{
        width: size,
        height: size,
        borderRadius: size / 3.4,
        background: "linear-gradient(145deg, var(--color-accent), var(--color-accent-deep))",
      }}
    >
      <Icons.logo size={Math.round(size * 0.54)} className="text-white" />
    </div>
  );
}

const tabs = [
  { href: "/workflows", label: "Workflows" },
  { href: "/executions", label: "Executions" },
  { href: "/credentials", label: "Credentials", disabled: true },
];

/**
 * The list-screen topbar. The editor deliberately does not use it — there the
 * breadcrumb replaces the tabs so the canvas keeps the full width.
 */
export function Topbar() {
  const pathname = usePathname();
  const router = useRouter();
  const { data } = useQuery({ queryKey: ["me"], queryFn: auth.me, retry: false });

  const initials = (data?.user.email ?? "")
    .split("@")[0]
    .split(/[._-]/)
    .slice(0, 2)
    .map((p) => p[0]?.toUpperCase() ?? "")
    .join("");

  return (
    <header className="flex h-14 shrink-0 items-center gap-6 border-b border-line bg-panel px-6">
      <Link href="/workflows" className="flex items-center gap-2.5 text-ink hover:text-ink">
        <Logo />
        <span className="text-[15px] font-bold tracking-[-0.01em]">Flowgrid</span>
      </Link>

      <nav className="flex items-center gap-1">
        {tabs.map((t) => {
          const active = pathname.startsWith(t.href);
          if (t.disabled) {
            return (
              <span
                key={t.href}
                title="Available in phase 3"
                className="cursor-not-allowed rounded-[9px] px-3.5 py-[7px] text-[13px] text-ink-5"
              >
                {t.label}
              </span>
            );
          }
          return (
            <Link
              key={t.href}
              href={t.href}
              className={clsx(
                "rounded-[9px] px-3.5 py-[7px] text-[13px]",
                active ? "bg-white/7 font-semibold text-ink" : "text-ink-3 hover:text-ink-2",
              )}
            >
              {t.label}
            </Link>
          );
        })}
      </nav>

      <div className="grow" />

      <div className="flex w-[190px] items-center gap-2.5 rounded-[9px] border border-line bg-input px-3 py-[7px]">
        <Icons.search size={14} className="text-ink-4" />
        <span className="text-[12.5px] text-ink-4">Search</span>
        <div className="grow" />
        <Kbd>/</Kbd>
      </div>

      <button
        type="button"
        onClick={async () => {
          await auth.logout().catch(() => undefined);
          router.push("/login");
        }}
        className="flex items-center gap-2.5"
        title="Sign out"
      >
        <span className="flex h-[30px] w-[30px] items-center justify-center rounded-full bg-[#2d3446] text-xs font-bold text-ink-2">
          {initials || "?"}
        </span>
        <Icons.chevronDown className="text-ink-4" />
      </button>
    </header>
  );
}

/** Page header used by both list screens. */
export function PageHeader({
  title,
  subtitle,
  action,
}: {
  title: string;
  subtitle?: string;
  action?: React.ReactNode;
}) {
  return (
    <div className="flex items-end gap-4">
      <div className="flex flex-col gap-1.5">
        <h1 className="text-[27px] font-extrabold tracking-[-0.03em]">{title}</h1>
        {subtitle && <p className="text-[13.5px] text-ink-3">{subtitle}</p>}
      </div>
      <div className="grow" />
      {action}
    </div>
  );
}
