import { useState } from "react";
import { Eye } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { TrustedHTML } from "@/components/shared/TrustedHTML";

type Props = {
  initial: string;
  onSave: (html: string) => Promise<void>;
};

// HtmlSectionEditor manages the operator-published landing-page HTML.
// Plain textarea + side-by-side preview; the preview goes through the
// same DOMPurify sanitizer as the public landing render so admins see
// exactly what visitors will see.
export function HtmlSectionEditor({ initial, onSave }: Props) {
  const [draft, setDraft] = useState(initial);
  const [preview, setPreview] = useState(initial);
  const [saving, setSaving] = useState(false);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Landing HTML</CardTitle>
        <p className="text-sm text-muted-foreground">
          Operator-trusted HTML rendered above the catalog stats on the
          landing page. Sanitised via DOMPurify before render — script
          tags and event handlers are stripped automatically.
        </p>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="space-y-1.5">
          <Label htmlFor="html-section">HTML</Label>
          <Textarea
            id="html-section"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            rows={8}
            placeholder="<p><strong>Welcome</strong> — request access by emailing...</p>"
          />
        </div>
        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            variant="secondary"
            size="sm"
            onClick={() => setPreview(draft)}
          >
            <Eye className="mr-2 h-4 w-4" /> Refresh preview
          </Button>
          <Button
            type="button"
            size="sm"
            disabled={saving}
            onClick={async () => {
              setSaving(true);
              try {
                await onSave(draft);
                setPreview(draft);
              } finally {
                setSaving(false);
              }
            }}
          >
            {saving ? "Saving…" : "Publish HTML"}
          </Button>
        </div>
        {preview && (
          <div className="rounded-md border border-border bg-card p-4">
            <p className="mb-2 text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">
              Preview
            </p>
            <TrustedHTML html={preview} className="prose prose-invert text-sm text-muted-foreground" />
          </div>
        )}
      </CardContent>
    </Card>
  );
}
