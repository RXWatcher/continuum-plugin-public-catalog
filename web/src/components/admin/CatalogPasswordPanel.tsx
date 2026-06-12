import { useState } from "react";
import { KeyRound, ShieldOff } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import type { PluginConfig } from "@/lib/types";

type Props = {
  config: PluginConfig;
  onSave: (patch: Partial<PluginConfig>) => Promise<void>;
  onClear: () => Promise<void>;
};

// CatalogPasswordPanel exposes the three orthogonal actions the
// rewrite added: set/change the password, toggle the "required" flag
// without losing the stored hash, and clear the password entirely.
// Status badges show the live state so admins know what they're
// looking at without having to remember.
export function CatalogPasswordPanel({ config, onSave, onClear }: Props) {
  const [password, setPassword] = useState("");
  const [savingPassword, setSavingPassword] = useState(false);
  const [togglePending, setTogglePending] = useState(false);
  const [clearPending, setClearPending] = useState(false);

  const hasHash = Boolean(config.catalog_password_set);

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between gap-3">
          <CardTitle>Catalog password</CardTitle>
          <div className="flex gap-1.5">
            <Badge variant={hasHash ? "default" : "outline"}>
              {hasHash ? "Set" : "Not set"}
            </Badge>
            <Badge variant={config.catalog_password_required ? "default" : "secondary"}>
              {config.catalog_password_required ? "Required" : "Open"}
            </Badge>
          </div>
        </div>
        <p className="text-sm text-muted-foreground">
          Gate or open the public catalog. Toggle "Require password" off without losing the stored
          value to re-enable it later.
        </p>
      </CardHeader>
      <CardContent className="space-y-5">
        <form
          className="space-y-3"
          onSubmit={async (e) => {
            e.preventDefault();
            if (!password) return;
            setSavingPassword(true);
            try {
              await onSave({ catalog_password: password, catalog_password_required: true });
              setPassword("");
            } finally {
              setSavingPassword(false);
            }
          }}
        >
          <div className="space-y-1.5">
            <Label htmlFor="new-password">{hasHash ? "Change password" : "Set password"}</Label>
            <Input
              id="new-password"
              type="password"
              autoComplete="new-password"
              placeholder="Enter a new password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              Stored as a bcrypt hash. The plaintext is never persisted and the previous
              hash is replaced.
            </p>
          </div>
          <Button type="submit" disabled={!password || savingPassword} size="sm">
            <KeyRound className="mr-2 h-4 w-4" />
            {savingPassword ? "Saving…" : hasHash ? "Update password" : "Save password"}
          </Button>
        </form>

        <div className="flex items-start justify-between gap-3 rounded-md border border-border bg-card px-3 py-3">
          <div className="space-y-1">
            <Label htmlFor="require-toggle" className="cursor-pointer text-sm font-medium">
              Require password to browse
            </Label>
            <p className="text-xs text-muted-foreground">
              Off opens the catalog without losing the stored hash.
            </p>
          </div>
          <Switch
            id="require-toggle"
            checked={config.catalog_password_required}
            disabled={togglePending}
            onCheckedChange={async (next) => {
              setTogglePending(true);
              try {
                await onSave({ catalog_password_required: next });
              } finally {
                setTogglePending(false);
              }
            }}
          />
        </div>

        {hasHash && (
          <div className="flex items-start justify-between gap-3 rounded-md border border-destructive/30 bg-destructive/5 px-3 py-3">
            <div className="space-y-1">
              <p className="text-sm font-medium text-destructive">Delete catalog password</p>
              <p className="text-xs text-muted-foreground">
                Zeros the stored hash and turns the requirement off. Outstanding session
                cookies stay valid until they're rotated by changing the token secret.
              </p>
            </div>
            <Button
              variant="destructive"
              size="sm"
              disabled={clearPending}
              onClick={async () => {
                if (!confirm("Delete the stored catalog password? Anyone with the URL will be able to browse.")) {
                  return;
                }
                setClearPending(true);
                try {
                  await onClear();
                } finally {
                  setClearPending(false);
                }
              }}
            >
              <ShieldOff className="mr-2 h-4 w-4" />
              {clearPending ? "Clearing…" : "Delete"}
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
