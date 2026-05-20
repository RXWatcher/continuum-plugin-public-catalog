import { useState } from "react";

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
// are read-only on this surface — they need a plugin restart, so the
// admin sees them but can't accidentally change them mid-run.
export function SettingsPanel({ value, onSave }: Props) {
  const [form, setForm] = useState<Partial<PluginConfig>>({
    public_base_url: value.public_base_url ?? "",
    ebook_installation_id: value.ebook_installation_id ?? "",
    audio_installation_id: value.audio_installation_id ?? "",
    token_ttl_hours: value.token_ttl_hours,
  });
  const [submitting, setSubmitting] = useState(false);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Plugin settings</CardTitle>
        <p className="text-sm text-muted-foreground">
          Public base URL, federated source IDs, and bypass-link expiry.
        </p>
      </CardHeader>
      <CardContent>
        <form
          className="space-y-4"
          onSubmit={async (e) => {
            e.preventDefault();
            setSubmitting(true);
            try {
              await onSave(form);
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
