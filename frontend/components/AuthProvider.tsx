"use client";

import { useEffect, useSyncExternalStore } from "react";
import { usePathname, useRouter } from "next/navigation";
import { Loader2 } from "lucide-react";
import { useAuthStore } from "@/store/auth";
import { Sidebar } from "@/components/layout/Sidebar";
import { Header } from "@/components/layout/Header";
import { SSEProvider } from "@/components/SSEProvider";

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const token = useAuthStore((s) => s.token);
  const hydrated = useSyncExternalStore(
    (onStoreChange) => {
      const unsub = useAuthStore.persist.onFinishHydration(onStoreChange);
      return unsub;
    },
    () => useAuthStore.persist.hasHydrated(),
    () => false
  );

  useEffect(() => {
    if (!hydrated) return;
    const isLogin = pathname.startsWith("/login");
    if (!token && !isLogin) {
      router.replace("/login");
    } else if (token && isLogin) {
      router.replace("/");
    }
  }, [hydrated, token, pathname, router]);

  if (!hydrated) {
    return <FullPageLoader />;
  }

  const isLogin = pathname.startsWith("/login");
  if (isLogin) {
    if (token) return <FullPageLoader />;
    return <>{children}</>;
  }

  if (!token) {
    return <FullPageLoader />;
  }

  return (
    <SSEProvider>
      <div className="flex h-full">
        <Sidebar />
        <div className="flex flex-col flex-1 min-w-0">
          <Header />
          <main className="flex-1 flex flex-col overflow-auto min-h-0">
            {children}
          </main>
        </div>
      </div>
    </SSEProvider>
  );
}

function FullPageLoader() {
  return (
    <div className="h-full flex items-center justify-center">
      <Loader2 className="w-6 h-6 animate-spin text-muted-foreground" />
    </div>
  );
}
