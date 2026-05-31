# Examples & cookbook

A practical, copy-pasteable walkthrough — from a fresh Tailscale account to
sharing several real dev servers through one URL.

---

## 0. One-time Tailscale setup

1. **Install Tailscale** and sign in:
   - Download: <https://tailscale.com/download>
   - Then bring the node up (opens a browser to sign up / log in):
     ```bash
     tailscale up
     ```

2. **Enable HTTPS certificates** for your tailnet (needed for Serve/Funnel):
   - Admin console → **DNS** → enable **MagicDNS** and **HTTPS Certificates**.
   - Docs: <https://tailscale.com/kb/1153/enabling-https>

3. **Enable Funnel** (only needed for *public* sharing) by granting the `funnel`
   node attribute in your tailnet policy file (admin console → **Access controls**):
   ```jsonc
   {
     "nodeAttrs": [
       { "target": ["autogroup:member"], "attr": ["funnel"] }
     ]
   }
   ```
   - Docs: <https://tailscale.com/kb/1223/funnel>

4. **Verify everything** with the built-in checker:
   ```bash
   npx tailscale-proxy doctor
   ```
   ```
   ✓ tailscale installed  (1.98.2)
   ✓ tailscale up
   ✓ funnel enabled
   ✓ service discovery  (3 service(s) in 3000-5000)

   All checks passed — you're ready to `tsp start`.
   ```

> Tip: `npm i -g tailscale-proxy` (or `brew install meabed/tap/tsp`) so you can
> type `tsp` instead of `npx tailscale-proxy`.

---

## 1. Start some dev servers

`tsp` discovers anything **listening on a port in range** (default `3000-5000`).
The URL path is the **project folder name**, so run each from its own directory.

These runtimes are **discovered by default**: `node`, `bun`, `deno`, `python`,
`ruby`, `php`, `go`, `java`, `dotnet`, `elixir`, `perl`, and `docker`-published
ports. Anything else (or a non-standard binary) is included with `--all`.

```bash
# JavaScript / TypeScript
cd ~/sites/portfolio && npx serve -l 3000            # static (node)
cd ~/sites/docs      && npx http-server -p 3001      # static (node)
cd ~/apps/web        && npx next dev -p 4000         # Next.js (node)
cd ~/apps/api        && bun run dev                  # bun (e.g. :4100)

# Python (interpreter or app servers — uvicorn/gunicorn are detected too)
cd ~/sites/blog      && python3 -m http.server 3003
cd ~/apps/fastapi    && uvicorn main:app --port 3010

# PHP / Ruby / Go / Java
cd ~/apps/legacy     && php -S 127.0.0.1:3004
cd ~/apps/rb         && ruby -run -e httpd . -p 3005
cd ~/apps/gosrv      && go run .                     # `go run` is detected
cd ~/apps/spring     && ./gradlew bootRun            # java (e.g. :8080 — widen --ports)

# Docker-published ports show up as docker-<port>
docker run -p 3030:80 nginx
```

Pick a subset with `--runtimes` (e.g. `--runtimes node,bun,python`), or cast the
widest net with `--all` (every listener in range, including unrecognized binaries
like compiled Go/Rust apps).

> Compiled binaries (a built Go or Rust server) have arbitrary process names, so
> they show up only under `--all` — `go run`/`cargo` dev workflows are detected.

---

## 2. Share them

### Public (Funnel) — the default

```bash
tsp                            # discover :3000-5000 and expose publicly
```

```
Using config: /Users/me/.tailscale-proxy/config.json
  ports=3000-5000  mode=public (Funnel)  proxy=127.0.0.1:8443  https=443
  interval=20s  runtimes=default (node,bun,deno,python,ruby,php,go,java,…)  deregister-after=5 scans  log-requests=true
  host=local (apps see localhost)

✓ tailscale installed  (1.98.2)
✓ tailscale up
✓ funnel enabled
✓ service discovery  (3 service(s) in 3000-5000)
Tailscale Funnel (public) → 127.0.0.1:8443 (port 443)

Services:
  https://bigfoot.tail-scale.ts.net/portfolio/  →  127.0.0.1:3000
  https://bigfoot.tail-scale.ts.net/docs/       →  127.0.0.1:3001
  https://bigfoot.tail-scale.ts.net/web/         →  127.0.0.1:4000

2026/05/31 02:41:04 200 GET    /web/ → 127.0.0.1:4000 (6ms)
```

Open any of those URLs from anywhere. <kbd>Ctrl-C</kbd> resets the Funnel.

### Private (Serve) — tailnet-only

```bash
tsp --private                  # only devices on your tailnet can reach it
```

---

## 3. Save your preferences (so you can just run `tsp`)

```bash
# Scan a wider range, include python, expose privately:
tsp configure --ports 3000-9000 --runtimes node,bun,python --private

# Now a bare `tsp` uses all of that:
tsp
```

The config lives at `~/.tailscale-proxy/config.json`. Flags always override it
for a single run.

---

## 4. Handy one-liners

```bash
tsp list                       # what's discoverable right now + the public URLs
tsp status                     # Serve/Funnel status + the service map
tsp reset                      # take the Funnel down (tsp reset --private for Serve)
tsp doctor                     # re-check tailscale / funnel / discovery
tsp update                     # update tsp to the latest release

tsp --ports 4000               # scan a single port
tsp --ports 8000-8999          # a different range
tsp --port 9000                # run the local proxy on :9000 (not :8443)
tsp --interval 5               # re-scan every 5s (faster pickup of new servers)
tsp --proxy-only               # run the proxy only; print the tailscale command
tsp --bg                       # run detached; logs → ./tsp.log
tsp --forward-host             # send the public host to apps (X-Forwarded-Host/Proto)
tsp --quiet                    # no per-request logs
```

---

## 5. A real multi-service setup (monorepo-style)

Say you're running four services, each in its own folder under `~/work/help-ai`:

```bash
cd ~/work/help-ai/services/agent     && bun run dev    # :3087
cd ~/work/help-ai/services/crawlee   && bun run dev    # :3120
cd ~/work/help-ai/services/workspace && bun run dev    # :3122
cd ~/work/help-ai/apps/web           && npm run dev    # :4501
```

```bash
tsp
```

```
Services:
  https://bigfoot.tail-scale.ts.net/agent/      →  127.0.0.1:3087
  https://bigfoot.tail-scale.ts.net/crawlee/    →  127.0.0.1:3120
  https://bigfoot.tail-scale.ts.net/workspace/  →  127.0.0.1:3122
  https://bigfoot.tail-scale.ts.net/web/        →  127.0.0.1:4501
```

Restart a server and `tsp` picks up the new process automatically (it keeps the
old route for a few scans to avoid flapping). When one project folder runs more
than one server, the process on the **lowest port** becomes the main `/<slug>/` and
the others get a `-<port>` suffix — all stay reachable, and `tsp` prints the map:

```
Note — these projects expose multiple services (main + suffixed):
  ~/work/help-ai/apps/web:
    /web/       →  :3087 (bun, pid 78327)   [main]
    /web-4983/  →  :4983 (node, pid 79001)
```

(A single process listening on several ports collapses to its lowest port.)

---

## 6. Notes

- The first path segment is the **project folder name** — run each server from
  its project directory so the slug is meaningful.
- Apps render exactly like `localhost` because prefix-less requests (`/_next/...`,
  `/api/...`, HMR) follow a per-browser **route cookie**. Use one app per browser
  profile if you want several open at once. See
  [TROUBLESHOOTING.md](TROUBLESHOOTING.md).
- Funnel public ports are `443`, `8443`, `10000` only (`--https-port`). Serve
  (private) can use any HTTPS port.
