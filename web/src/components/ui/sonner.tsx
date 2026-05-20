import type { CSSProperties } from "react";
import { Toaster as SonnerToaster, type ToasterProps } from "sonner";

const LIGHT_THEMES = new Set(["cinema-light"]);

// Toaster picks light vs dark from the data-theme attribute the SPA
// bootstrap writes onto <html>. No global auth-state lookup needed.
const Toaster = ({ ...props }: ToasterProps) => {
  const theme = typeof document !== "undefined" ? document.documentElement.dataset.theme ?? "" : "";
  const sonnerTheme = LIGHT_THEMES.has(theme) ? "light" : "dark";

  return (
    <SonnerToaster
      theme={sonnerTheme}
      className="toaster group"
      toastOptions={{
        classNames: {
          toast: "!text-[var(--popover-foreground)]",
          title: "!text-[var(--popover-foreground)]",
          description: "!text-[var(--popover-foreground)]",
        },
      }}
      style={
        {
          "--normal-bg": "var(--popover)",
          "--normal-text": "var(--popover-foreground)",
          "--normal-border": "var(--border)",
        } as CSSProperties
      }
      {...props}
    />
  );
};

export { Toaster };
