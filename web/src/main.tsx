import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { App } from "@/App";
import { captureTokenFromURL } from "@/lib/authToken";
import "./index.css";

// Capture any one-shot ?token=... from the URL before React mounts so
// it doesn't leak through referrers or screen sharing. lib/api attaches
// the captured token to subsequent fetches automatically.
captureTokenFromURL();

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
