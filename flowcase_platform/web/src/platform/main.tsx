import "@fontsource-variable/inter";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { createHashRouter, RouterProvider } from "react-router-dom";
import { Platform } from "./Platform";
import { parseAllowedOrigins, startHostBridge } from "../api/hostBridge";
startHostBridge(parseAllowedOrigins(import.meta.env.VITE_HOST_ORIGINS));
import "../../../../packages/ui/tokens.css";
import "./platform.css";

const root = document.getElementById("root");
if (!root) throw new Error("Missing root element");
const router = createHashRouter([{ path: "*", element: <Platform /> }]);
createRoot(root).render(
  <StrictMode>
    <RouterProvider router={router} />
  </StrictMode>,
);

import "./launchpad.css";

import "./connected.css";
