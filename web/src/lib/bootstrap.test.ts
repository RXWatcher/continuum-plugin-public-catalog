import { afterEach, describe, expect, it } from "vitest";
import { readBootstrap } from "./bootstrap";

// readBootstrap is the SPA's only entry point for server-baked state.
// These tests pin the safe-fallback behaviour so a missing or malformed
// bootstrap can never render the SPA into an inconsistent state.

describe("readBootstrap", () => {
  afterEach(() => {
    document.getElementById("public-catalog-bootstrap")?.remove();
  });

  it("returns landing defaults when the bootstrap element is missing", () => {
    expect(document.getElementById("public-catalog-bootstrap")).toBeNull();
    const bootstrap = readBootstrap();
    expect(bootstrap.mode).toBe("landing");
    expect(bootstrap.theme).toBe("default");
    expect(bootstrap.authRequired).toBe(false);
  });

  it("returns landing defaults when the bootstrap is the unrendered template placeholder", () => {
    seedBootstrap("%PUBLIC_CATALOG_BOOTSTRAP%");
    const bootstrap = readBootstrap();
    expect(bootstrap.mode).toBe("landing");
  });

  it("parses a populated bootstrap JSON payload", () => {
    seedBootstrap(
      JSON.stringify({
        mode: "catalog",
        theme: "cinema-light",
        catalogHref: "catalog",
        authRequired: true,
        token: "abc",
      }),
    );
    const bootstrap = readBootstrap();
    expect(bootstrap.mode).toBe("catalog");
    expect(bootstrap.theme).toBe("cinema-light");
    expect(bootstrap.authRequired).toBe(true);
    expect(bootstrap.token).toBe("abc");
  });

  it("falls back to landing defaults when the bootstrap JSON is malformed", () => {
    seedBootstrap("{not json");
    const bootstrap = readBootstrap();
    expect(bootstrap.mode).toBe("landing");
  });
});

function seedBootstrap(content: string) {
  const node = document.createElement("script");
  node.id = "public-catalog-bootstrap";
  node.type = "application/json";
  node.textContent = content;
  document.body.appendChild(node);
}
