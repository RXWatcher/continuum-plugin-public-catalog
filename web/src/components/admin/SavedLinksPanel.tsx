import { useState } from "react";
import { Copy, Link2, Plus, Trash2 } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { absoluteURL } from "@/lib/api";
import type { SavedCatalogLink } from "@/lib/types";

type Props = {
  links: SavedCatalogLink[];
  onCreate: (req: {
    saveName: string;
    libraryIds: string[];
    mediaTypes: string[];
    html: string;
  }) => Promise<void>;
  onDelete: (id: number) => Promise<void>;
};

// SavedLinksPanel hosts the bypass-link manager: create a named scope
// (library + media-type + custom HTML), copy/paste-able URLs, delete.
// The split UI makes the scoping intent obvious — none of the original
// admin HTML's cryptic "media types comma-separated" experience.
export function SavedLinksPanel({ links, onCreate, onDelete }: Props) {
  const [name, setName] = useState("");
  const [libraryIds, setLibraryIds] = useState("");
  const [mediaTypes, setMediaTypes] = useState("");
  const [html, setHtml] = useState("");
  const [submitting, setSubmitting] = useState(false);

  return (
    <Card className="lg:col-span-2">
      <CardHeader>
        <CardTitle>Bypass links</CardTitle>
        <p className="text-sm text-muted-foreground">
          Named catalog URLs that skip the password gate for specific
          recipients. Each link can be scoped to libraries and media
          types, and can carry its own custom HTML.
        </p>
      </CardHeader>
      <CardContent className="space-y-6">
        <form
          className="grid gap-3 lg:grid-cols-2"
          onSubmit={async (e) => {
            e.preventDefault();
            if (!name.trim()) return;
            setSubmitting(true);
            try {
              await onCreate({
                saveName: name.trim(),
                libraryIds: parseList(libraryIds),
                mediaTypes: parseList(mediaTypes),
                html,
              });
              setName("");
              setLibraryIds("");
              setMediaTypes("");
              setHtml("");
            } finally {
              setSubmitting(false);
            }
          }}
        >
          <div className="space-y-1.5">
            <Label htmlFor="link-name">Link name</Label>
            <Input
              id="link-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Press preview – studio"
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="link-libs">Allowed library IDs (comma-separated, blank = all)</Label>
            <Input
              id="link-libs"
              value={libraryIds}
              onChange={(e) => setLibraryIds(e.target.value)}
              placeholder="1, 4"
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="link-types">Allowed media types (comma-separated, blank = all)</Label>
            <Input
              id="link-types"
              value={mediaTypes}
              onChange={(e) => setMediaTypes(e.target.value)}
              placeholder="movie, series"
            />
          </div>
          <div className="space-y-1.5 lg:col-span-2">
            <Label htmlFor="link-html">Custom landing HTML for this link</Label>
            <Textarea
              id="link-html"
              value={html}
              onChange={(e) => setHtml(e.target.value)}
              rows={3}
              placeholder="<p>Welcome — this preview link expires after the screening.</p>"
            />
          </div>
          <div className="lg:col-span-2">
            <Button type="submit" disabled={submitting || !name.trim()}>
              <Plus className="mr-2 h-4 w-4" />
              {submitting ? "Saving…" : "Create bypass link"}
            </Button>
          </div>
        </form>

        {links.length === 0 ? (
          <div className="rounded-md border border-dashed border-border p-6 text-center text-sm text-muted-foreground">
            <p className="font-medium text-foreground">No bypass links yet.</p>
            <p>Create one above to share scoped catalog access.</p>
          </div>
        ) : (
          <ul className="space-y-2">
            {links.map((link) => (
              <li
                key={link.id}
                className="rounded-md border border-border bg-card p-3"
              >
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="min-w-0 space-y-1">
                    <div className="flex items-center gap-2">
                      <Link2 className="h-3.5 w-3.5 text-muted-foreground" />
                      <p className="truncate font-medium">{link.name}</p>
                    </div>
                    <p className="break-all font-mono text-xs text-muted-foreground">
                      {absoluteURL(link.url)}
                    </p>
                    <div className="flex flex-wrap gap-1.5 pt-1">
                      {(link.mediaTypes ?? []).map((mt) => (
                        <Badge key={mt} variant="secondary">
                          {mt}
                        </Badge>
                      ))}
                      {(link.libraryIds ?? []).map((id) => (
                        <Badge key={`lib-${id}`} variant="outline">
                          lib {id}
                        </Badge>
                      ))}
                      {(!link.mediaTypes?.length && !link.libraryIds?.length) && (
                        <Badge variant="outline">All scope</Badge>
                      )}
                    </div>
                  </div>
                  <div className="flex gap-2">
                    <Button
                      type="button"
                      size="sm"
                      variant="secondary"
                      onClick={() => navigator.clipboard.writeText(absoluteURL(link.url))}
                    >
                      <Copy className="mr-2 h-4 w-4" />
                      Copy URL
                    </Button>
                    <Button
                      type="button"
                      size="sm"
                      variant="destructive"
                      onClick={async () => {
                        if (!confirm(`Delete bypass link "${link.name}"?`)) return;
                        await onDelete(link.id);
                      }}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </div>
                </div>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}

function parseList(value: string): string[] {
  return value
    .split(/[,\n]+/)
    .map((s) => s.trim())
    .filter(Boolean);
}
