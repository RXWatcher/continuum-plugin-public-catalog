import { readBootstrap } from "@/lib/bootstrap";
import { Toaster } from "@/components/ui/sonner";
import { AdminPage } from "@/pages/Admin";
import { CatalogPage } from "@/pages/Catalog";
import { DetailPage } from "@/pages/Detail";
import { LandingPage } from "@/pages/Landing";

// App routes by the server-baked bootstrap.mode (set by the Go handler
// that rendered the SPA shell), rather than by client-side react-router.
// The server already knows which surface the visitor asked for and the
// SPA only has to render the matching page tree — keeps the public
// pages crawlable and pre-renderable on the first paint.
export function App() {
  const bootstrap = readBootstrap();

  let page;
  switch (bootstrap.mode) {
    case "admin":
      page = <AdminPage />;
      break;
    case "catalog":
      page = <CatalogPage bootstrap={bootstrap} />;
      break;
    case "detail":
      page = <DetailPage bootstrap={bootstrap} />;
      break;
    default:
      page = <LandingPage bootstrap={bootstrap} />;
  }

  return (
    <>
      {page}
      <Toaster />
    </>
  );
}
