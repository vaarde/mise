import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

const root = document.getElementById("root");
if (!root) throw new Error("missing #root");
const appRoot = createRoot(root);

const hasLiveApi = Boolean((import.meta.env.VITE_API_BASE_URL as string | undefined)?.trim());
const localDemo = ["1", "true", "yes"].includes(
  ((import.meta.env.VITE_LOCAL_DEMO as string | undefined) ?? "").trim().toLowerCase(),
);

// The console never substitutes fixture data for the live estate. Without an
// API it says so unless local demo mode is explicitly requested.
function NotConfigured() {
  return (
    <main className="standalone">
      <div className="box">
        <span className="brand-mark" aria-hidden>M</span>
        <h1>Mise API not configured</h1>
        <p className="muted">
          Set <code className="mono">VITE_API_BASE_URL</code> to the Mise public API and rebuild. No stand-in data is shown.
        </p>
      </div>
    </main>
  );
}

async function render() {
  if (localDemo) {
    document.body.classList.add("demo");
    await import("./styles.css");
    await import("./polished.css");
    const { default: LocalDemoApp } = await import("./PolishedApp.js");
    appRoot.render(
      <StrictMode>
        <LocalDemoApp />
      </StrictMode>,
    );
    return;
  }

  document.body.classList.add("mise");
  await import("./live/mise.css");
  const { default: LiveApp } = await import("./LiveApp.js");
  appRoot.render(
    <StrictMode>
      {hasLiveApi ? <LiveApp /> : <NotConfigured />}
    </StrictMode>,
  );
}

void render();
