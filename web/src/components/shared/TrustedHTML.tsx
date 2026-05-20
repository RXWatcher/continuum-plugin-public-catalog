import { useMemo } from "react";
import DOMPurify from "isomorphic-dompurify";

type Props = {
  html: string;
  className?: string;
};

// TrustedHTML renders operator-supplied HTML. The plugin's admin-only
// writer (every write goes through requireAdmin server-side) means the
// markup author is a real Continuum admin — but as defence in depth we
// still run it through DOMPurify so a compromised admin session or a
// browser extension can't smuggle script content into other visitors'
// pages.
export function TrustedHTML({ html, className }: Props) {
  const sanitized = useMemo(() => DOMPurify.sanitize(html, { USE_PROFILES: { html: true } }), [html]);
  if (!sanitized) return null;
  return <div className={className} dangerouslySetInnerHTML={{ __html: sanitized }} />;
}
