import type { PublicBootstrap } from "@/lib/types";

// readBootstrap returns the per-request payload the Go server bakes
// into <script id="public-catalog-bootstrap" type="application/json">.
// On missing / malformed bootstrap, returns a safe landing-mode default
// so the SPA still renders something usable.
export function readBootstrap(): PublicBootstrap {
  if (typeof document === "undefined") {
    return defaultBootstrap();
  }
  const node = document.getElementById("public-catalog-bootstrap");
  if (!node || !node.textContent || node.textContent.includes("%PUBLIC_CATALOG_BOOTSTRAP%")) {
    return defaultBootstrap();
  }
  try {
    const parsed = JSON.parse(node.textContent) as Partial<PublicBootstrap>;
    return {
      mode: parsed.mode ?? "landing",
      theme: parsed.theme ?? "default",
      catalogHref: parsed.catalogHref ?? "catalog",
      customHTML: parsed.customHTML ?? "",
      authRequired: Boolean(parsed.authRequired),
      token: parsed.token ?? "",
      initialStats: parsed.initialStats,
      initialLibraryId: parsed.initialLibraryId,
      initialItems: parsed.initialItems,
      initialNextPageToken: parsed.initialNextPageToken,
      initialTotalCount: parsed.initialTotalCount,
      initialFilters: parsed.initialFilters,
    };
  } catch {
    return defaultBootstrap();
  }
}

function defaultBootstrap(): PublicBootstrap {
  return {
    mode: "landing",
    theme: "default",
    catalogHref: "catalog",
    customHTML: "",
    authRequired: false,
    token: "",
  };
}
