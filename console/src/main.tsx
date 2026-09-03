import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import PolishedApp from "./PolishedApp.js";
import "./styles.css";
import "./polished.css";

const root = document.getElementById("root");
if (!root) throw new Error("missing #root");

createRoot(root).render(
  <StrictMode>
    <PolishedApp />
  </StrictMode>,
);
