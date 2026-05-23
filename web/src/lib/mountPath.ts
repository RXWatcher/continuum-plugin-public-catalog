// Silo mounts each plugin under /api/v1/plugins/{installationId}/.
// The plugin's SPA can't know that mount path at build time, so this
// helper pulls it out of window.location.pathname at runtime.

export function extractMountPath(pathname: string): string {
  const match = pathname.match(/^(\/api\/v1\/plugins\/[^/]+)/);
  return match ? match[1] : "";
}

export function mountPath(): string {
  return extractMountPath(window.location.pathname);
}
