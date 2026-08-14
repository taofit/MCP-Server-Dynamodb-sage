import { useAuthStore } from "@/store/auth";

export function getToken(): string | null {
  return useAuthStore.getState().token;
}

export function apiLogin(token: string): Promise<{ role: string }> {
  return fetch("/api/login", {
    method: "POST",
    headers: {
      Authorization: `Bearer ${token}`,
    },
  }).then((res) => {
    if (!res.ok) {
      throw new Error("Invalid token");
    }
    return res.json();
  });
}

export function logout() {
  useAuthStore.getState().clearAuth();
  if (typeof window !== "undefined") {
    window.location.replace("/login");
  }
}

export async function apiFetch(input: string, init?: RequestInit): Promise<Response> {
  const token = getToken();
  const headers = new Headers(init?.headers);
  if (token) {
    headers.set("Authorization", `Bearer ${token}`);
  }
  const res = await fetch(input, { ...init, headers });
  if (res.status === 401) {
    logout();
    throw new Error("Unauthorized");
  }
  return res;
}

export function sseUrl(path: string): string {
  const token = getToken();
  if (!token) return path;
  return `${path}${path.includes("?") ? "&" : "?"}token=${encodeURIComponent(token)}`;
}
