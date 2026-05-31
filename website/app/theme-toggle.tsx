"use client";

import { useEffect, useState } from "react";

const Sun = (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
    <circle cx="12" cy="12" r="4" />
    <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
  </svg>
);

const Moon = (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z" />
  </svg>
);

export function ThemeToggle() {
  // Resolved theme. Starts matching the no-FOUC script (data-theme or system).
  const [dark, setDark] = useState(false);

  useEffect(() => {
    const set = document.documentElement.dataset.theme;
    const resolved = set ?? (window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");
    setDark(resolved === "dark");
  }, []);

  function toggle() {
    const next = dark ? "light" : "dark";
    setDark(!dark);
    document.documentElement.dataset.theme = next;
    try {
      localStorage.setItem("tsp-theme", next);
    } catch {}
  }

  return (
    <button
      type="button"
      onClick={toggle}
      className="tsp-toggle"
      aria-label={dark ? "Switch to light theme" : "Switch to dark theme"}
      title="Toggle light / dark"
    >
      {dark ? Sun : Moon}
    </button>
  );
}
