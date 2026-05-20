// Small set of display formatters shared across pages.

export function formatCount(n: number): string {
  if (!Number.isFinite(n) || n < 1000) return String(n ?? 0);
  return new Intl.NumberFormat(undefined).format(n);
}

export function formatShortDate(value: string | undefined): string {
  if (!value) return "";
  try {
    return new Intl.DateTimeFormat(undefined, {
      dateStyle: "medium",
    }).format(new Date(value));
  } catch {
    return value;
  }
}

export function formatRuntimeMinutes(minutes: number | undefined): string {
  if (!minutes || minutes <= 0) return "";
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  if (h <= 0) return `${m}m`;
  if (m <= 0) return `${h}h`;
  return `${h}h ${m}m`;
}

export function titleCase(value: string): string {
  if (!value) return "";
  return value.charAt(0).toUpperCase() + value.slice(1);
}
