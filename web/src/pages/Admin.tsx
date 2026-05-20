import { useEffect, useState } from "react";
import { ExternalLink, RefreshCcw } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { SettingsPanel } from "@/components/admin/SettingsPanel";
import { CatalogPasswordPanel } from "@/components/admin/CatalogPasswordPanel";
import { HtmlSectionEditor } from "@/components/admin/HtmlSectionEditor";
import { SavedLinksPanel } from "@/components/admin/SavedLinksPanel";
import { TopBar } from "@/components/shared/TopBar";
import {
  clearCatalogPassword,
  createCatalogToken,
  deleteCatalogLink,
  getAdminConfig,
  getHtmlSection,
  listCatalogLinks,
  saveHtmlSection,
  updateAdminConfig,
} from "@/api/admin";
import type { PluginConfig, SavedCatalogLink } from "@/lib/types";

// AdminPage replaces the legacy Go-string admin HTML with a React
// composition. Each section owns one config concern; the page itself
// is just orchestration (loading + toast + reload-on-success).
export function AdminPage() {
  const [config, setConfig] = useState<PluginConfig | null>(null);
  const [html, setHtml] = useState("");
  const [links, setLinks] = useState<SavedCatalogLink[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  async function reload() {
    setLoading(true);
    try {
      const [cfg, htmlResp, linksResp] = await Promise.all([
        getAdminConfig(),
        getHtmlSection(),
        listCatalogLinks(),
      ]);
      setConfig(cfg);
      setHtml(htmlResp.html ?? "");
      setLinks(linksResp ?? []);
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load admin data");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void reload();
  }, []);

  async function handleSaveConfig(patch: Partial<PluginConfig>) {
    try {
      const next = await updateAdminConfig(patch);
      setConfig(next);
      toast.success("Settings saved.");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to save settings");
    }
  }

  async function handleClearPassword() {
    try {
      await clearCatalogPassword();
      const refreshed = await getAdminConfig();
      setConfig(refreshed);
      toast.success("Catalog password cleared.");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to clear password");
    }
  }

  async function handleSaveHTML(next: string) {
    try {
      await saveHtmlSection(next);
      setHtml(next);
      toast.success("Landing HTML published.");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to publish HTML");
    }
  }

  async function handleCreateLink(req: {
    saveName: string;
    libraryIds: string[];
    mediaTypes: string[];
    html: string;
  }) {
    try {
      const resp = await createCatalogToken(req);
      if (resp.savedLink) {
        setLinks((current) => [resp.savedLink!, ...current.filter((l) => l.id !== resp.savedLink!.id)]);
      }
      toast.success("Bypass link created.");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to create link");
    }
  }

  async function handleDeleteLink(id: number) {
    try {
      await deleteCatalogLink(id);
      setLinks((current) => current.filter((l) => l.id !== id));
      toast.success("Bypass link deleted.");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to delete link");
    }
  }

  return (
    <main className="min-h-[100dvh] bg-background text-foreground">
      <div className="mx-auto max-w-6xl space-y-6 px-4 py-8 md:px-8">
        <TopBar
          eyebrow="Plugin administration"
          title="Public Catalog admin"
          subtitle="Manage the public catalog gate, edit the landing HTML, and issue bypass links."
          trailing={
            <>
              <Button
                size="icon"
                variant="ghost"
                aria-label="Refresh"
                onClick={() => void reload()}
              >
                <RefreshCcw className="h-4 w-4" />
              </Button>
              <Button asChild variant="secondary">
                <a href="../" target="_blank" rel="noreferrer">
                  Open public page
                  <ExternalLink className="ml-2 h-4 w-4" />
                </a>
              </Button>
            </>
          }
        />

        {error && (
          <div className="rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">
            {error}
          </div>
        )}

        {loading || !config ? (
          <p className="text-sm text-muted-foreground">Loading…</p>
        ) : (
          <div className="grid gap-6 lg:grid-cols-2">
            <SettingsPanel value={config} onSave={handleSaveConfig} />
            <CatalogPasswordPanel
              config={config}
              onSave={handleSaveConfig}
              onClear={handleClearPassword}
            />
            <HtmlSectionEditor initial={html} onSave={handleSaveHTML} />
            <SavedLinksPanel
              links={links}
              onCreate={handleCreateLink}
              onDelete={handleDeleteLink}
            />
          </div>
        )}
      </div>
    </main>
  );
}
