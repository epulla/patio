import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import { initTheme } from "./theme";
import { initLanguage } from "./i18n";
import { pruneDrafts } from "./drafts";
import "./styles.css";

initTheme();
initLanguage();
pruneDrafts();

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
