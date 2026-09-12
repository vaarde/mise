import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import EverydayApp from "./EverydayApp.js";
import LiveApp from "./LiveApp.js";
import "./styles.css";
import "./polished.css";
import "./everyday.css";

const root = document.getElementById("root");
if (!root) throw new Error("missing #root");

const hasLiveApi = Boolean((import.meta.env.VITE_API_BASE_URL as string | undefined)?.trim());

createRoot(root).render(
  <StrictMode>
    {hasLiveApi ? <LiveApp /> : <EverydayApp />}
  </StrictMode>,
);
