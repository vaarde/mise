import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import EverydayApp from "./EverydayApp.js";
import "./styles.css";
import "./polished.css";
import "./everyday.css";

const root = document.getElementById("root");
if (!root) throw new Error("missing #root");

createRoot(root).render(
  <StrictMode>
    <EverydayApp />
  </StrictMode>,
);
