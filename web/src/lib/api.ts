import { mountPath } from "@/lib/mountPath";
import { authHeaders } from "@/lib/authToken";
import type { APIError } from "@/lib/types";

// api<T> is the one fetch helper every typed API module uses.
// On 2xx: returns parsed JSON.
// On non-2xx: throws an APIError carrying status + the backend error
//             envelope code so callers can branch on specific cases.
export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${mountPath()}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...authHeaders(),
      ...(init?.headers ?? {}),
    },
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    const message = data?.error?.message || data?.message || `Request failed (${res.status})`;
    const error = new Error(message) as APIError;
    error.responseStatus = res.status;
    error.responseCode = data?.error?.code;
    throw error;
  }
  return data as T;
}

// absoluteURL turns a relative plugin URL into an absolute one,
// folding in the runtime mount path so links rendered to clipboard
// or print are clickable from anywhere.
export function absoluteURL(url: string): string {
  if (!url) return "";
  if (url.startsWith("http://") || url.startsWith("https://")) return url;
  if (url.startsWith("/")) {
    return new URL(`${mountPath()}${url}`, window.location.origin).toString();
  }
  return new URL(url, window.location.href).toString();
}
