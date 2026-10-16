import React from "react";
import { createRoot } from "react-dom/client";
import "@fontsource-variable/geist";
import "@fontsource-variable/geist-mono";
import "@fontsource-variable/bricolage-grotesque/opsz.css";
import App from "./App";
import "../app.css";

createRoot(document.getElementById("root")).render(<React.StrictMode><App /></React.StrictMode>);
