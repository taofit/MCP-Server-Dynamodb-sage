"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  MessageSquare,
  LayoutDashboard,
  Activity,
  BarChart3,
  Wrench,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { Logo } from "./Logo";

const navItems = [
  { href: "/", label: "Overview", icon: LayoutDashboard },
  { href: "/chat/", label: "Chat", icon: MessageSquare },
  { href: "/activity/", label: "Activity", icon: Activity },
  { href: "/monitoring/", label: "Monitoring", icon: BarChart3 },
  { href: "/tools/", label: "Tools", icon: Wrench },
];

export function Sidebar() {
  const pathname = usePathname();

  return (
    <aside className="hidden md:flex w-60 flex-col border-r border-sidebar-border bg-sidebar/80">
      {/* Logo */}
      <div className="flex items-center gap-2.5 px-5 py-4 border-b border-sidebar-border">
        <Logo className="w-6 h-6" />
        <span className="font-semibold text-sm tracking-tight text-sidebar-foreground">
          DynamoDB Sage
        </span>
      </div>

      {/* Navigation */}
      <nav className="flex-1 px-3 py-4 space-y-1">
        {navItems.map((item) => {
          const active =
            item.href === "/"
              ? pathname === "/"
              : pathname.startsWith(item.href);
          return (
            <Link
              key={item.href}
              href={item.href}
              className={cn(
                "flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-colors",
                active
                  ? "bg-sidebar-accent text-sidebar-accent-foreground"
                  : "text-sidebar-foreground/70 hover:text-sidebar-foreground hover:bg-sidebar-accent/50"
              )}
            >
              <item.icon className="w-4 h-4 shrink-0 text-sidebar-foreground/50" />
              {item.label}
            </Link>
          );
        })}
      </nav>

      {/* Footer */}
      <div className="px-5 py-3 border-t border-sidebar-border">
        <p className="text-[11px] text-sidebar-foreground/40">v1.0.0</p>
      </div>
    </aside>
  );
}
