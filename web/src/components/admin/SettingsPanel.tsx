import { useState } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { PluginConfig } from "@/lib/types";

type Props = {
  value: PluginConfig;
  onSave: (patch: Partial<PluginConfig>) => Promise<void>;
};

// SettingsPanel edits the operator-tunable bits of plugin config that
// AREN'T the password (that lives in its own card). Listener settings
// are surfaced read-only — they need a plugin restart, so the admin
// sees what's bound but can't change it through the API
// (hUpdateConfig server-side rejects the change with the same wording).
export function SettingsPanel({ value, onSave }: Props) {
  const [form, setForm] = useState<Partial<PluginConfig>>({
    public_base_url: value.public_base_url ?? "",
    ebook_installation_id: value.ebook_installation_id ?? "",
    audio_installation_id: value.audio_installation_id ?? "",
    token_ttl_hours: value.token_ttl_hours,
    public_library_ids: value.public_library_ids ?? [],
  });
  const [libraryIdsText, setLibraryIdsText] = useState(
    (value.public_library_ids ?? []).join(", "),
  );
  const [submitting, setSubmitting] = useState(false);

  const listenerLabel = formatListener(value);
  const listenerWildcard = isWildcardListen(value.standalone_http_listen, value.public_port);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Plugin settings</CardTitle>
        <p className="text-sm text-muted-foreground">
          Public base URL, federated source IDs, and bypass-link expiry.
        </p>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="rounded-md border border-border bg-card px-3 py-3">
          <div className="flex items-center justify-between gap-2">
            <p className="text-sm font-medium">Standalone HTTP listener</p>
            <Badge variant={listenerLabel === "Not configured" ? "outline" : "secondary"}>
              {listenerLabel}
            </Badge>
          </div>
          <p className="mt-1 text-xs text-muted-foreground">
            Set in the manifest (<code>public_port</code> /{" "}
            <code>standalone_http_listen</code>). Changes require a plugin restart.
          </p>
          {listenerWildcard && (
            <p className="mt-2 text-xs text-amber-600 dark:text-amber-400">
              Listener bound to <strong>all interfaces</strong>. Prefer
              <code className="mx-1">127.0.0.1:port</code> behind a reverse proxy
              unless this is intentional.
            </p>
          )}
        </div>

        <form
          className="space-y-4"
          onSubmit={async (e) => {
            e.preventDefault();
            setSubmitting(true);
            try {
              const ids = libraryIdsText
                .split(",")
                .map((s) => s.trim())
                .filter((s) => /^[0-9]+$/.test(s));
              await onSave({ ...form, public_library_ids: ids });
            } finally {
              setSubmitting(false);
            }
          }}
        >
          <div className="space-y-1.5">
            <Label htmlFor="public-base-url">Public base URL</Label>
            <Input
              id="public-base-url"
              value={form.public_base_url ?? ""}
              onChange={(e) => setForm({ ...form, public_base_url: e.target.value })}
              placeholder="https://example.com/api/v1/plugins/public-catalog"
            />
            <p className="text-xs text-muted-foreground">
              Absolute URL used when generated share links are returned. Leave empty for plugin-relative URLs.
            </p>
          </div>

          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="ebook-id">Ebook installation ID</Label>
              <Input
                id="ebook-id"
                value={form.ebook_installation_id ?? ""}
                onChange={(e) => setForm({ ...form, ebook_installation_id: e.target.value })}
                placeholder="42"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="audio-id">Audiobook installation ID</Label>
              <Input
                id="audio-id"
                value={form.audio_installation_id ?? ""}
                onChange={(e) => setForm({ ...form, audio_installation_id: e.target.value })}
                placeholder="43"
              />
            </div>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="public-library-ids">Publicly exposable library IDs</Label>
            <Input
              id="public-library-ids"
              value={libraryIdsText}
              onChange={(e) => setLibraryIdsText(e.target.value)}
              placeholder="1, 2, 5"
            />
            <p className="text-xs text-muted-foreground">
              Comma-separated host library (media folder) IDs allowed in the public
              catalog. This is a hard allowlist:{" "}
              <strong>leave empty to expose nothing</strong>. Bypass-link scopes can
              only narrow this set, never widen it.
            </p>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="ttl">Default bypass link TTL (hours)</Label>
            <Input
              id="ttl"
              type="number"
              min={1}
              value={form.token_ttl_hours ?? 168}
              onChange={(e) => setForm({ ...form, token_ttl_hours: Number(e.target.value) })}
            />
          </div>

          <div>
            <Button type="submit" disabled={submitting}>
              {submitting ? "Saving…" : "Save settings"}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

function formatListener(value: PluginConfig): string {
  const explicit = value.standalone_http_listen?.trim();
  if (explicit) return explicit;
  if (value.public_port && value.public_port > 0) return `:${value.public_port}`;
  return "Not configured";
}

function isWildcardListen(addr: string | undefined, port: number | undefined): boolean {
  if (!addr || !addr.trim()) return Boolean(port && port > 0);
  // ":8090" / "0.0.0.0:8090" / "[::]:8090" bind every interface.
  if (/^:\d+$/.test(addr.trim())) return true;
  if (/^0\.0\.0\.0:\d+$/.test(addr.trim())) return true;
  if (/^\[::\]:\d+$/.test(addr.trim())) return true;
  return false;
}
