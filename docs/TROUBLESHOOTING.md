# Troubleshooting

Start with `tsp doctor` — it diagnoses the most common setup problems and prints
the exact fix link for each.

## "It works from my phone but not from my Mac" (the MagicDNS gotcha)

**Symptom:** from the same machine running `tsp`, opening
`https://<node>.ts.net/<project>/` behaves oddly or hits a different server than
expected, even though `tsp list` shows the route.

**Why:** from the host, MagicDNS resolves `<node>.ts.net` to your **tailnet IP**
(`100.x`), so the request may be answered locally instead of traversing the public
Funnel relay.

**Fixes:**

- **Test from outside your tailnet** — a phone on cellular, another network, or an
  online "fetch this URL" tool. The public Funnel path works correctly there.
- **Force the public ingress from the host** by bypassing MagicDNS:

  ```bash
  PUBIP=$(dig +short <node>.ts.net @1.1.1.1 | head -1)   # public Funnel ingress IP
  curl -s -i --resolve "<node>.ts.net:443:$PUBIP" "https://<node>.ts.net/<project>/"
  ```

## No services found

`tsp` only registers listeners whose runtime is a known web runtime (`node`, `bun`,
`deno`) within the port range. If `tsp list` is empty:

- Make sure your dev server is actually listening in range (default `3000-5000`).
- Widen the range: `tsp start --ports 3000-9000` (a single port also works:
  `--ports 4000`).
- Include non-web processes: `tsp start --all`, or add runtimes:
  `tsp start --runtimes node,bun,python`.

## `lsof` not found (macOS/Linux)

Discovery uses `lsof`. Install it: `apt install lsof` / `dnf install lsof`. macOS
ships it. `tsp doctor` reports this under "service discovery".

## A page loads without styles / behaves differently than localhost

Fixed by cookie route-affinity: visiting `…/<slug>/` sets a `tsp_route` cookie so
that tab's prefix-less requests (`/_next/...`, `/api/...`, HMR) reach the right
backend. If a page still looks wrong:

- Make sure you opened it via its **`/<slug>/`** URL (that's what sets the cookie),
  not a bare asset URL.
- Don't actively use two different apps in the **same browser** at once — affinity
  is per-browser. Use separate browsers/profiles, or re-visit the `/<slug>/` URL to
  switch which app the tab is pinned to.

## The same project shows on multiple ports / a slug has a `-<port>` suffix

This is expected. The slug is the nearest project-root folder (the one with
`package.json`/`.git`/etc.). Within one project folder, `tsp` keeps the process on
the **lowest port** as the "main" service (clean slug) and gives any *other* process
in the same folder a `-<port>` suffix so it stays reachable — e.g. `bun dev` on
`:3087` → `myapp/`, while an aux tool on `:4983` → `myapp-4983/`. A single process
listening on several ports is collapsed to its lowest port (one entry). Two
**distinct** projects that share a folder name also get a `-<port>` suffix to stay
unique. Run `tsp list` for the canonical slugs.

## A service disappeared but the route lingers (or vice-versa)

By design, a service missing from discovery is kept for `--deregister-cycles`
(default 5) scans before its route is removed — this prevents flapping when a dev
server restarts. Lower it (`--deregister-cycles 1`) for immediate removal, or raise
it for longer grace. New services appear on the next scan (`--interval`, default 20s;
lower it for faster pickup).

## Reaching services from a container ("Failed to resolve …ts.net")

`Failed to resolve 'bigfoot.quoll-adhara.ts.net'` from a container means the
container's DNS can't resolve that name. **The right fix depends on where the
container runs and whether you used Funnel (public) or Serve (`--private`).**

`*.ts.net` names resolve two different ways:

- **Funnel (public, the default)** — the name is in **public DNS**. Any host with
  normal internet DNS resolves it, tailnet or not.
- **Serve (`--private`)** — the name only resolves via **MagicDNS inside your
  tailnet**, and it points at a `100.x` address only routable by tailnet members.

### Container on the **same machine** as `tsp` (e.g. Docker Desktop)

Skip DNS entirely — talk to the proxy directly by path. Bind it to a reachable
address:

```bash
tsp --bind 0.0.0.0            # proxy now listens on all interfaces
```
```bash
# inside the container — routes /<slug>/ to your local dev server:
curl http://host.docker.internal:8443/<slug>/        # Docker Desktop (mac/win)
# Linux: docker run --add-host=host.docker.internal:host-gateway ...
#        (or use the docker bridge gateway IP, often 172.17.0.1)
```

`container → host tsp → 127.0.0.1:<service>`, no DNS or tailnet involved.
⚠ `0.0.0.0` exposes the proxy to your LAN — bind a specific interface
(e.g. `--bind 172.17.0.1`, the docker bridge) to narrow it. `host.docker.internal`
only points at the **same host**, so this does **not** work for a remote pod.

### Container on a **remote host / Kubernetes** (the usual cause)

A remote pod can't reach `host.docker.internal`, so use the exposure URL — but how
you make it resolve depends on the mode:

**If you exposed publicly (Funnel — default):** the name is in public DNS, so a pod
with working internet DNS should already resolve it. If it still fails, the pod's
resolver is locked down — pin the name to the public Funnel ingress IP:

```bash
dig +short bigfoot.quoll-adhara.ts.net @1.1.1.1     # → public ingress IP, e.g. 209.177.145.192
```
```yaml
# Kubernetes pod spec — the equivalent of docker --add-host:
spec:
  hostAliases:
    - ip: "209.177.145.192"
      hostnames: ["bigfoot.quoll-adhara.ts.net"]
```
```bash
# plain Docker on a remote host:
docker run --add-host "bigfoot.quoll-adhara.ts.net:209.177.145.192" ...
```

#### When the remote host is **itself on the tailnet** (MagicDNS shadows the name)

This is the subtle one. If the consuming host runs Tailscale with MagicDNS enabled
(the default, `--accept-dns=true`), Tailscale's resolver `100.100.100.100` answers
`*.ts.net` queries with the node's **tailnet `100.x` address** — *not* the public
Funnel ingress. Traffic then goes over the tailnet to your node's `tailscaled`,
which only serves that path if you set up **Serve**; a **Funnel-only** node won't
answer it. Containers on that host make it worse — they usually don't inherit
`100.100.100.100`, so they fall back to public DNS, which may still be negatively
cached and return `NXDOMAIN`. That mismatch is exactly this:

```bash
nslookup bigfoot.quoll-adhara.ts.net 8.8.8.8           # NXDOMAIN (not cached yet)
nslookup bigfoot.quoll-adhara.ts.net 100.100.100.100   # → tailnet IP / funnel IP via MagicDNS
```

`tsp doctor` prints an advisory `magicdns` note whenever this node has `accept-dns`
on, as a reminder of this gotcha.

The Funnel name **is** in public DNS, so the fix is to stop MagicDNS from shadowing
it on the **consuming host** (the remote one — not the machine running `tsp`):

```bash
tailscale set --accept-dns=false      # use public DNS for *.ts.net; re-enable with =true
```

```bash
dig +short bigfoot.quoll-adhara.ts.net @8.8.8.8         # now confirm: → 209.177.145.192
```

If you'd rather have `tsp` flip it for you when it starts (opt-in, off by default),
pass `tsp --accept-dns=false`. It persists after `tsp` exits — revert with
`tailscale set --accept-dns=true`.

Prefer not to touch the host's DNS? Fix just the container instead — point it at
MagicDNS, or pin the public IP:

```bash
docker run --dns 100.100.100.100 ...                                  # resolve like the host
docker run --add-host bigfoot.quoll-adhara.ts.net:209.177.145.192 ... # or skip DNS entirely
```

> **Why `tsp` doesn't flip `--accept-dns` for you:** it's a **global, persistent,
> machine-wide** Tailscale setting that disables MagicDNS for *every* `*.ts.net`
> name (breaking name resolution for your other tailnet nodes), and it belongs on
> the **consumer** host — usually a different machine from the one running `tsp`.
> A dev proxy silently changing your system's DNS as a side effect would be
> surprising and out of scope, so it's left as a deliberate, reversible step.

**If you exposed privately (`--private` Serve):** the name and its `100.x` address
only work **from inside the tailnet**, so a remote pod must *join* the tailnet —
there's no public IP to point at. Either:

- Run **Tailscale in the pod** (a sidecar container, or the Tailscale Kubernetes
  operator / subnet router). Once the pod is on the tailnet, MagicDNS resolves the
  name and `100.x` routes normally — nothing else to change. ([k8s operator docs](https://tailscale.com/kb/1185/kubernetes))
- Or, if the pod can already route `100.x`, map the name to the tailnet IP so the
  HTTPS cert still matches:
  ```bash
  # tailnet IP of the node running tsp:  tailscale ip -4
  docker run --add-host "bigfoot.quoll-adhara.ts.net:100.x.y.z" ...
  ```
  (k8s: the same `hostAliases` block, with the `100.x` IP.)

**Rule of thumb:** remote container that must stay off the tailnet → use **Funnel**
and resolve the public name. Remote container you can put **on** the tailnet → use
**Serve** with a Tailscale sidecar and let MagicDNS do its job.

## `502` upstream error

The registered dev server isn't accepting connections (crashed or exited between
scans). The body names the failed `127.0.0.1:<port>`. It clears on the next scan once
the server is back (or after the de-register debounce if it stays down).

## Funnel still on after a hard kill

`tsp` resets Serve/Funnel synchronously on `Ctrl-C`. After a `kill -9`, clear it
manually:

```bash
tsp reset                # or: tsp reset --private   (for Serve)
tailscale serve status   # should print "No serve config"
```

## Port already in use

`tsp` listens on `--port` (default `8443`). If something else holds it:
`tsp start --port 9000`.

## Background mode

`tsp start --bg` detaches and logs to `./tsp.log`. Stop it by `kill`-ing the printed
pid (or `pgrep -f "tsp"`), then `tsp reset` to be sure the Serve/Funnel entry is down.
