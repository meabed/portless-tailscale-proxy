"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

export const pages = [
  { href: "/", label: "Introduction" },
  { href: "/installation", label: "Installation" },
  { href: "/getting-started", label: "Getting started" },
  { href: "/usage", label: "Usage & commands" },
  { href: "/configuration", label: "Configuration" },
  { href: "/how-it-works", label: "How it works" },
  { href: "/troubleshooting", label: "Troubleshooting" },
];

export function Sidebar() {
  const pathname = usePathname();
  return (
    <nav className="tsp-sidebar" aria-label="Documentation">
      <span className="tsp-sidebar__label">Documentation</span>
      {pages.map((p) => (
        <Link
          key={p.href}
          href={p.href}
          className={pathname === p.href ? "active" : ""}
          aria-current={pathname === p.href ? "page" : undefined}
        >
          {p.label}
        </Link>
      ))}
    </nav>
  );
}
