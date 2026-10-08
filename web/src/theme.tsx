import { useState } from "react";

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
    <button
      className="ghost icon"
      onClick={toggle}
      aria-label={`Switch to ${theme === "dark" ? "light" : "dark"} theme`}
      title={`Switch to ${theme === "dark" ? "light" : "dark"} theme`}
    >
      {theme === "dark" ? "☀" : "☾"}
    </button>
  );
}
