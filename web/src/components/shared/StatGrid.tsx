import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { formatCount } from "@/lib/format";
import type { CatalogStats } from "@/lib/types";

type Props = {
  stats: CatalogStats | null;
  loading?: boolean;
};

// StatGrid renders the totals + per-library + per-type + per-quality
// counts pulled from /api/public/stats. Used by the landing page and
// (eventually) the admin overview.
export function StatGrid({ stats, loading }: Props) {
  if (loading || !stats) {
    return (
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-24 w-full" />
        ))}
      </div>
    );
  }

  const entries: Array<{ label: string; value: number; kind: string }> = [
    { label: "Total items", value: stats.totalItems ?? 0, kind: "total" },
  ];
  for (const lib of stats.libraryCounts ?? []) {
    entries.push({ label: lib.libraryName, value: lib.count, kind: "library" });
  }
  for (const mt of stats.mediaTypeCounts ?? []) {
    entries.push({ label: mt.mediaType, value: mt.count, kind: "media" });
  }
  for (const q of stats.qualityCounts ?? []) {
    entries.push({ label: q.label, value: q.count, kind: "quality" });
  }

  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4">
      {entries.map((entry) => (
        <Card key={`${entry.kind}-${entry.label}`} className="overflow-hidden">
          <CardContent className="space-y-2 py-4">
            <p className="text-[10px] font-semibold uppercase tracking-[0.18em] text-muted-foreground">
              {entry.kind}
            </p>
            <p className="text-2xl font-semibold leading-none md:text-3xl">{formatCount(entry.value)}</p>
            <p className="truncate text-sm text-muted-foreground">{entry.label}</p>
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
