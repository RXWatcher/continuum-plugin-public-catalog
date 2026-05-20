import { api } from "@/lib/api";
import type { CatalogStats } from "@/lib/types";

export function getPublicStats(): Promise<CatalogStats> {
  return api<CatalogStats>("/api/public/stats");
}
