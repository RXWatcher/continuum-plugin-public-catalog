import { useState } from "react";
import { Lock } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { catalogLogin } from "@/api/catalog";

type Props = {
  onAuthorized: () => void;
};

// AuthPrompt is the password gate the catalog page shows when no
// bypass-token query param or session cookie is present and the
// catalog is configured to require a password.
export function AuthPrompt({ onAuthorized }: Props) {
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setSubmitting(true);
    setError("");
    try {
      await catalogLogin(password);
      onAuthorized();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Login failed");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <main className="flex min-h-[100dvh] items-center justify-center px-4 py-12">
      <Card className="w-full max-w-md">
        <CardHeader>
          <div className="flex items-center gap-2 text-xs font-semibold uppercase tracking-[0.16em] text-accent">
            <Lock className="h-3.5 w-3.5" /> Public catalog
          </div>
          <CardTitle className="text-2xl">Enter the catalog password</CardTitle>
          <p className="text-sm text-muted-foreground">
            The operator has gated the public catalog behind a shared password.
            Ask them for it, or open the share link they sent you.
          </p>
        </CardHeader>
        <CardContent>
          <form className="space-y-4" onSubmit={submit}>
            <div className="space-y-1.5">
              <Label htmlFor="catalog-password">Password</Label>
              <Input
                id="catalog-password"
                type="password"
                autoComplete="current-password"
                autoFocus
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                aria-invalid={Boolean(error)}
              />
            </div>
            {error && <p className="text-sm text-destructive">{error}</p>}
            <Button type="submit" className="w-full" disabled={submitting}>
              {submitting ? "Unlocking…" : "Open catalog"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </main>
  );
}
