import { Library } from "lucide-react";

type Props = {
  homeHref?: string;
  eyebrow?: string;
  title?: string;
  subtitle?: string;
  trailing?: React.ReactNode;
};

// TopBar is the shared header used by landing / catalog / detail.
// Renders a small left-aligned brand line and accepts arbitrary trailing
// content (a stat count, a settings link, etc.).
export function TopBar({ homeHref = "./", eyebrow, title, subtitle, trailing }: Props) {
  return (
    <header className="flex flex-col gap-4 border-b border-border/60 pb-6 md:flex-row md:items-end md:justify-between">
      <div className="flex flex-col gap-2">
        <a
          href={homeHref}
          className="text-muted-foreground hover:text-foreground inline-flex w-fit items-center gap-2 text-xs font-medium uppercase tracking-[0.16em]"
        >
          <Library className="h-3.5 w-3.5" />
          Silo public catalog
        </a>
        {eyebrow && (
          <p className="text-xs font-semibold uppercase tracking-[0.16em] text-accent">{eyebrow}</p>
        )}
        {title && (
          <h1 className="text-3xl font-semibold leading-tight md:text-4xl lg:text-5xl">{title}</h1>
        )}
        {subtitle && (
          <p className="max-w-2xl text-sm leading-relaxed text-muted-foreground md:text-base">
            {subtitle}
          </p>
        )}
      </div>
      {trailing && <div className="flex items-center gap-3">{trailing}</div>}
    </header>
  );
}
