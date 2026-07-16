"use client";

import { useTheme } from "./ThemeProvider";
import { Sun, Moon, Menu } from "lucide-react";
import { Button } from "@/components/ui/button";
import Link from "next/link";
import {
  MessageSquare,
  LayoutDashboard,
  Activity,
  BarChart3,
  Wrench,
} from "lucide-react";
import { useState } from "react";
import { cn } from "@/lib/utils";
import { usePathname } from "next/navigation";

const mobileNavItems = [
  { href: "/", label: "Home", icon: LayoutDashboard },
  { href: "/chat/", label: "Chat", icon: MessageSquare },
  { href: "/activity/", label: "Activity", icon: Activity },
  { href: "/monitoring/", label: "Metrics", icon: BarChart3 },
  { href: "/tools/", label: "Tools", icon: Wrench },
];

export function Header() {
  const { theme, toggle } = useTheme();
  const [mobileOpen, setMobileOpen] = useState(false);
  const pathname = usePathname();

  return (
    <header className="flex items-center justify-between px-4 h-14 border-b border-border bg-background/80 backdrop-blur-sm">
      {/* Mobile menu button */}
      <Button
        variant="ghost"
        size="icon"
        className="md:hidden"
        onClick={() => setMobileOpen(!mobileOpen)}
      >
        <Menu className="w-5 h-5" />
      </Button>

      {/* Mobile title */}
      <span className="md:hidden text-sm font-semibold">DynamoDB Sage</span>

      {/* Spacer for desktop */}
      <div className="hidden md:block" />

      {/* Theme toggle */}
      <Button variant="ghost" size="icon" onClick={toggle}>
        {theme === "dark" ? (
          <Sun className="w-5 h-5 text-muted-foreground" />
        ) : (
          <Moon className="w-5 h-5 text-muted-foreground" />
        )}
      </Button>

      {/* Mobile nav dropdown */}
      {mobileOpen && (
        <div className="absolute top-14 left-0 right-0 z-50 bg-background border-b border-border md:hidden">
          <nav className="px-3 py-2 space-y-1">
            {mobileNavItems.map((item) => {
              const active =
                item.href === "/"
                  ? pathname === "/"
                  : pathname.startsWith(item.href);
              return (
                <Link
                  key={item.href}
                  href={item.href}
                  onClick={() => setMobileOpen(false)}
                  className={cn(
                    "flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-colors",
                    active
                      ? "bg-accent text-accent-foreground"
                      : "text-muted-foreground hover:text-foreground hover:bg-accent/50"
                  )}
                >
                  <item.icon className="w-4 h-4 shrink-0" />
                  {item.label}
                </Link>
              );
            })}
          </nav>
        </div>
      )}
    </header>
  );
}
