import Link from "next/link";
import { Head } from "nextra/components";
import "nextra-theme-docs/style.css";
import "./globals.css";
import { Sidebar } from "./nav";
import { ThemeToggle } from "./theme-toggle";

export const metadata = {
  title: {
    default: "tailscale-proxy — self-hosted ngrok alternative on Tailscale",
    template: "%s – tailscale-proxy",
  },
  description:
    "Discover local dev servers by port and expose them through one Tailscale Serve/Funnel entry, routed by project name. An open-source, self-hosted ngrok alternative.",
  metadataBase: new URL("https://tailscaleproxy.vercel.app"),
};

// Set the theme before first paint to avoid a flash; honors the stored choice,
// otherwise falls through to the CSS prefers-color-scheme default.
const noFlash = `(function(){try{var d=document.documentElement,t=localStorage.getItem('tsp-theme');if(t==='dark'||t==='light'){d.dataset.theme=t}var dark=t?t==='dark':matchMedia('(prefers-color-scheme: dark)').matches;d.classList.toggle('dark',dark);d.classList.toggle('light',!dark)}catch(e){}})();`;

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en" dir="ltr" suppressHydrationWarning>
      <Head />
      <body>
        <script dangerouslySetInnerHTML={{ __html: noFlash }} />
        <header className="tsp-header">
          <div className="tsp-header__inner">
            <Link href="/" className="tsp-brand">
              <span className="tsp-brand__dot" aria-hidden="true" />
              tailscale-proxy
            </Link>
            <nav className="tsp-topnav" aria-label="Primary">
              <a href="https://www.npmjs.com/package/tailscale-proxy" target="_blank" rel="noreferrer">
                npm
              </a>
              <a
                href="https://github.com/meabed/tailscale-proxy"
                target="_blank"
                rel="noreferrer"
                aria-label="GitHub repository"
                title="GitHub"
              >
                <svg width="19" height="19" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
                  <path d="M8 0a8 8 0 0 0-2.53 15.59c.4.07.55-.17.55-.38v-1.35c-2.23.49-2.7-1.07-2.7-1.07-.36-.92-.89-1.17-.89-1.17-.73-.5.05-.49.05-.49.8.06 1.23.83 1.23.83.72 1.23 1.87.87 2.33.67.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.83-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82a7.6 7.6 0 0 1 4 0c1.53-1.03 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.52.56.83 1.28.83 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48v2.2c0 .21.15.46.55.38A8 8 0 0 0 8 0z" />
                </svg>
              </a>
              <ThemeToggle />
            </nav>
          </div>
        </header>

        <div className="tsp-shell">
          <aside className="tsp-aside">
            <Sidebar />
          </aside>
          <main className="tsp-main">
            <article className="tsp-content">{children}</article>
            <footer className="tsp-footer">
              MIT © {new Date().getFullYear()} ·{" "}
              <a href="https://meabed.com" target="_blank" rel="noreferrer">
                Mohamed Meabed
              </a>{" "}
              ·{" "}
              <a href="https://github.com/meabed/tailscale-proxy" target="_blank" rel="noreferrer">
                Source
              </a>
            </footer>
          </main>
        </div>
      </body>
    </html>
  );
}
