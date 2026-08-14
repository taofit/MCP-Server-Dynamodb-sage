"use client";

import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { KeyRound, Loader2, Play, ShieldCheck } from "lucide-react";
import { Logo } from "@/components/layout/Logo";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { apiLogin } from "@/lib/api";
import { useAuthStore } from "@/store/auth";

export default function LoginPage() {
  const router = useRouter();
  const setAuth = useAuthStore((s) => s.setAuth);
  const [token, setToken] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [demoLoading, setDemoLoading] = useState(false);
  const [autoKey, setAutoKey] = useState<string | null>(null);
  const [autoAttempted, setAutoAttempted] = useState(false);

  const doDemo = useCallback(async () => {
    setDemoLoading(true);
    setError("");
    try {
      const res = await fetch("/api/demo");
      if (!res.ok) {
        throw new Error("Demo not available");
      }
      const data = await res.json();
      setAuth(data.token, data.role);
      router.replace("/");
    } catch {
      setError("Live demo is not available right now.");
    } finally {
      setDemoLoading(false);
    }
  }, [router, setAuth]);

  const doLogin = useCallback(async (raw: string) => {
    const trimmed = raw.trim();
    if (!trimmed) return;

    setLoading(true);
    setError("");
    try {
      const data = await apiLogin(trimmed);
      setAuth(trimmed, data.role);
      router.replace("/");
    } catch {
      setError("Invalid token. Please check your access key and try again.");
    } finally {
      setLoading(false);
    }
  }, [router, setAuth]);

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const key = params.get("key") ?? params.get("token");
    if (key) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setToken(key);
      setAutoKey(key);
      window.history.replaceState({}, "", "/login");
    }
  }, []);

  useEffect(() => {
    if (autoAttempted || autoKey === null) return;
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setAutoAttempted(true);
    doLogin(autoKey);
  }, [autoKey, autoAttempted, doLogin]);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    doLogin(token);
  };

  return (
    <div className="min-h-full flex items-center justify-center p-6">
      <div className="w-full max-w-sm">
        <div className="rounded-2xl border border-border bg-card/60 backdrop-blur-sm p-8 shadow-2xl">
          <div className="flex flex-col items-center gap-2 mb-6">
            <div className="flex items-center gap-2.5">
              <Logo className="w-8 h-8" />
              <span className="text-xl font-bold tracking-tight">
                DynamoDB Sage
              </span>
            </div>
            <p className="text-sm text-muted-foreground text-center mt-1">
              Explore your DynamoDB with AI-powered tools
            </p>
          </div>

          {error && (
            <p className="text-sm text-destructive text-center mb-4">{error}</p>
          )}

          <div className="space-y-4">
            <Button
              type="button"
              onClick={doDemo}
              disabled={loading || demoLoading}
              className="w-full h-11"
            >
              {demoLoading ? (
                <Loader2 className="w-4 h-4 animate-spin" />
              ) : (
                <Play className="w-4 h-4" />
              )}
              {demoLoading ? "Entering demo..." : "Try the live demo"}
            </Button>

            <div className="flex items-center gap-3 py-1">
              <div className="h-px flex-1 bg-border" />
              <span className="text-xs text-muted-foreground">or</span>
              <div className="h-px flex-1 bg-border" />
            </div>
          </div>

          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="relative">
              <KeyRound className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
              <Input
                type="password"
                value={token}
                onChange={(e) => setToken(e.target.value)}
                placeholder="Access key"
                autoFocus
                autoComplete="off"
                className="pl-9 h-11 text-base"
              />
            </div>

            <Button
              type="submit"
              disabled={loading || !token.trim()}
              className="w-full h-11"
            >
              {loading ? (
                <Loader2 className="w-4 h-4 animate-spin" />
              ) : (
                <ShieldCheck className="w-4 h-4" />
              )}
              {loading ? "Signing in..." : "Sign in"}
            </Button>
          </form>

          <p className="text-xs text-muted-foreground text-center mt-6">
            Tokens are issued by your DynamoDB Sage administrator.
          </p>
        </div>
      </div>
    </div>
  );
}
