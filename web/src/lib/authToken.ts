// captureTokenFromURL reads a one-shot bearer token from the URL on
// first load (used by the Continuum admin to hand us authentication
// without a redirect dance), strips it from the address bar via
// history.replaceState, and keeps it in memory for subsequent fetches.
// The token never lands in localStorage / sessionStorage so it doesn't
// outlive the tab.
let cachedToken = "";

export function captureTokenFromURL(): void {
  const params = new URLSearchParams(window.location.search);
  cachedToken = params.get("token") || "";

  const theme = params.get("theme") || sessionStorage.getItem("continuum-theme") || "";
  if (theme) {
    document.documentElement.dataset.theme = theme;
    try {
      sessionStorage.setItem("continuum-theme", theme);
    } catch {
      // private browsing tabs can't write — ignore
    }
  }

  if (!params.has("token")) return;
  params.delete("token");
  const clean =
    window.location.pathname +
    (params.toString() ? `?${params.toString()}` : "") +
    window.location.hash;
  window.history.replaceState(null, "", clean);
}

export function authHeaders(): Record<string, string> {
  return cachedToken ? { Authorization: `Bearer ${cachedToken}` } : {};
}
