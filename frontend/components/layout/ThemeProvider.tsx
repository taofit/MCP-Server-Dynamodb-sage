"use client";

import { createContext, useCallback, useContext, useEffect, useState } from "react";

type Theme = "dark" | "light";

interface ThemeState {
  theme: Theme;
  mounted: boolean;
}

const ThemeContext = createContext<{
  theme: Theme;
  toggle: () => void;
}>({ theme: "dark", toggle: () => {} });

export function useTheme() {
  return useContext(ThemeContext);
}

function getInitialTheme(): Theme {
  if (typeof window === "undefined") return "dark";
  const saved = localStorage.getItem("theme") as Theme | null;
  if (saved === "dark" || saved === "light") return saved;
  return window.matchMedia("(prefers-color-scheme: dark)").matches
    ? "dark"
    : "light";
}

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const [state, setState] = useState<ThemeState>({ theme: "dark", mounted: false });

  useEffect(() => {
    const initial = getInitialTheme();
    document.documentElement.classList.toggle("dark", initial === "dark");
    document.documentElement.classList.toggle("light", initial === "light");
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setState({ theme: initial, mounted: true });
  }, []);

  useEffect(() => {
    if (!state.mounted) return;
    document.documentElement.classList.toggle("dark", state.theme === "dark");
    document.documentElement.classList.toggle("light", state.theme === "light");
    localStorage.setItem("theme", state.theme);
  }, [state.theme, state.mounted]);

  const toggle = useCallback(() => {
    setState((prev) => {
      const next = prev.theme === "dark" ? "light" : "dark";
      document.documentElement.classList.toggle("dark", next === "dark");
      document.documentElement.classList.toggle("light", next === "light");
      localStorage.setItem("theme", next);
      return { ...prev, theme: next };
    });
  }, []);

  if (!state.mounted) {
    return <>{children}</>;
  }

  return (
    <ThemeContext.Provider value={{ theme: state.theme, toggle }}>
      {children}
    </ThemeContext.Provider>
  );
}
