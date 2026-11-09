import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { basePath } from "./api/client";
import { App } from "./app/App";
import "./styles/app.css";

const root = document.getElementById("root");
if (!root) throw new Error("missing #root");

createRoot(root).render(
  <StrictMode>
    <BrowserRouter basename={basePath || "/"}>
      <App />
    </BrowserRouter>
  </StrictMode>,
);
