import { useEffect, useState } from "react";
import { ArrowRight } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { StatGrid } from "@/components/shared/StatGrid";
import { TopBar } from "@/components/shared/TopBar";
import { TrustedHTML } from "@/components/shared/TrustedHTML";
import { getPublicStats } from "@/api/stats";
import type { CatalogStats, PublicBootstrap } from "@/lib/types";

type Props = { bootstrap: PublicBootstrap };

// LandingPage is the public marketing surface. Shows the operator-
// published HTML, a top-level stats grid, and a CTA into the catalog.
// initialStats comes from the server-rendered bootstrap so the first
// paint already has numbers; we refresh in the background to pick up
// updates between the SSR snapshot and the SPA mount.
export function LandingPage({ bootstrap }: Props) {
  const [stats, setStats] = useState<CatalogStats | null>(bootstrap.initialStats ?? null);
  const [loading, setLoading] = useState(!bootstrap.initialStats);

  useEffect(() => {
    let cancelled = false;
    getPublicStats()
      .then((next) => {
        if (!cancelled) setStats(next);
      })
      .catch(() => {
        // Stats are best-effort — the SSR bootstrap already filled in
        // the initial values for the first paint.
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <main className="min-h-[100dvh] bg-background text-foreground">
      <div className="mx-auto max-w-6xl space-y-10 px-4 py-10 md:px-8">
        <TopBar
          eyebrow="Public catalog"
          title="Browse what's available"
          subtitle="Use the catalog to confirm titles, qualities, and availability before requesting access."
          trailing={
            <Button asChild>
              <a href={bootstrap.catalogHref || "catalog"}>
                Open catalog <ArrowRight className="ml-2 h-4 w-4" />
              </a>
            </Button>
          }
        />

        <StatGrid stats={stats} loading={loading} />

        {bootstrap.customHTML && (
          <>
            <Separator />
            <TrustedHTML
              html={bootstrap.customHTML}
              className="prose prose-invert max-w-3xl text-muted-foreground"
            />
          </>
        )}
      </div>
    </main>
  );
}
