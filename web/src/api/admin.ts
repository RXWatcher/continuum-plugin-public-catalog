import { api } from "@/lib/api";
import type { PluginConfig, SavedCatalogLink } from "@/lib/types";

export function getAdminConfig(): Promise<PluginConfig> {
  return api<PluginConfig>("/api/admin/config");
}

// updateAdminConfig sends a partial config. Empty / omitted fields the
// backend treats as "leave unchanged". Setting catalog_password to a
// non-empty string triggers a fresh bcrypt hash on save.
export function updateAdminConfig(patch: Partial<PluginConfig>): Promise<PluginConfig> {
  return api<PluginConfig>("/api/admin/config", {
    method: "PATCH",
    body: JSON.stringify(patch),
  });
}

export function clearCatalogPassword(): Promise<{ ok: boolean }> {
  return api<{ ok: boolean }>("/api/admin/catalog-password", { method: "DELETE" });
}

export function getHtmlSection(): Promise<{ html: string }> {
  return api<{ html: string }>("/api/admin/html-section");
}

export function saveHtmlSection(html: string): Promise<{ ok: boolean }> {
  return api<{ ok: boolean }>("/api/admin/html-section", {
    method: "PUT",
    body: JSON.stringify({ html }),
  });
}

export type CreateTokenRequest = {
  libraryIds?: string[];
  mediaTypes?: string[];
  saveName?: string;
  html?: string;
};

export type CreateTokenResponse = {
  token: string;
  url: string;
  savedLink?: SavedCatalogLink;
};

export function createCatalogToken(req: CreateTokenRequest): Promise<CreateTokenResponse> {
  return api<CreateTokenResponse>("/api/admin/catalog-token", {
    method: "POST",
    body: JSON.stringify(req),
  });
}

export function listCatalogLinks(): Promise<SavedCatalogLink[]> {
  return api<SavedCatalogLink[]>("/api/admin/catalog-links");
}

export function deleteCatalogLink(id: number): Promise<{ ok: boolean }> {
  return api<{ ok: boolean }>(`/api/admin/catalog-links/${id}`, { method: "DELETE" });
}
