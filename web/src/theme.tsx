import { useState } from "react";
import { IconButton } from "./icons";

type Theme = "light" | "dark";

function currentTheme(): Theme {
  const saved = localStorage.getItem("theme");
  if (saved === "light" || saved === "dark") return saved;
  return matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

export function initTheme() {
  document.documentElement.dataset.theme = currentTheme();
}

export function ThemeToggle() {
  const [theme, setTheme] = useState(currentTheme);
  function toggle() {
    const next = theme === "dark" ? "light" : "dark";
    localStorage.setItem("theme", next);
    document.documentElement.dataset.theme = next;
    setTheme(next);
  }
  return (
    <IconButton
      className="ghost"
      icon={theme === "dark" ? "sun" : "moon"}
      label={`Switch to ${theme === "dark" ? "light" : "dark"} theme`}
      onClick={toggle}
    />
  );
}
