import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import LiveApp from "./LiveApp.js";
import "./live/mise.css";

const root = document.getElementById("root");
if (!root) throw new Error("missing #root");

document.body.classList.add("mise");

const hasLiveApi = Boolean((import.meta.env.VITE_API_BASE_URL as string | undefined)?.trim());

// The console never substitutes fixture data for the live estate. Without an
// API it says so, and points at the local mock for previewing the UI.
function NotConfigured() {
  return (
    <main className="standalone">
      <div className="card">
        <span className="brand-mark" aria-hidden>M</span>
        <h1>Mise isn't connected to an API</h1>
        <p className="muted">To preview the console with sample data, stop this server and run:</p>
        <pre className="raw" style={{ whiteSpace: "pre-wrap" }}>npm run dev:mock</pre>
        <p className="muted">To use the live AWS API, set <code className="mono">VITE_API_BASE_URL</code> in <code className="mono">console/.env.local</code> and restart <code className="mono">npm run dev</code>.</p>
      </div>
    </main>
  );
}

createRoot(root as HTMLElement).render(
  <StrictMode>
    {hasLiveApi ? <LiveApp /> : <NotConfigured />}
  </StrictMode>,
);
