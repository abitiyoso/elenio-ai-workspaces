import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { HashRouter } from "react-router-dom";
import { Platform } from "./Platform";
import "../../../../packages/ui/tokens.css";
import "./platform.css";

const root = document.getElementById("root");
if (!root) throw new Error("Missing root element");
createRoot(root).render(
  <StrictMode>
    <HashRouter>
      <Platform />
    </HashRouter>
  </StrictMode>,
);
