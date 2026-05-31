# Troubleshooting

Start with `ptp doctor` — it diagnoses the most common setup problems and prints
the exact fix link for each.

## "It works from my phone but not from my Mac" (the MagicDNS gotcha)

**Symptom:** from the same machine running `ptp`, opening
`https://<node>.ts.net/<host>.local/` returns a 404 with an `x-portless: 1`
header, even though `ptp` is running and `ptp list` shows the route.

**Why:** this is *not* a `ptp` bug. From the host machine, MagicDNS resolves
`<node>.ts.net` to your **tailnet IP** (e.g. `100.94.8.118`). portless's local
proxy binds `:443` on *all* interfaces, including that tailnet IP, so it answers
the request **directly** — the traffic never goes out to the public Funnel ingress
and back. Hence the `x-portless` page from portless instead of your proxy.

**Fixes:**

- **Test from outside your tailnet** — a phone on cellular, another network, or
  an online "fetch this URL" tool. The public Funnel path works correctly there.

- **Force the public ingress from the host.** Bypass MagicDNS by resolving the
  hostname via public DNS and pinning it:

  ```bash
  # Public Funnel ingress IP (NOT your 100.x tailnet IP):
  PUBIP=$(dig +short <node>.ts.net @1.1.1.1 | head -1)

  curl -s -i --resolve "<node>.ts.net:443:$PUBIP" \
    "https://<node>.ts.net/<host>.local/"
  ```

  A correct response is `200` (or your app's real status) with **no**
  `x-portless` header — proving traffic flowed Funnel → `ptp` → your dev server.

## `ptp start` exits: "preflight failed"

One of the doctor checks failed. Read the ✗ lines — each has a fix link:

- **tailscale installed** ✗ → install from <https://tailscale.com/download>.
- **tailscale up** ✗ → run `tailscale up`.
- **funnel enabled** ✗ → Funnel isn't enabled for your tailnet. Enable
  [HTTPS certificates](https://tailscale.com/kb/1153/enabling-https) and grant the
  `funnel` node attribute in your tailnet policy file
  ([Funnel docs](https://tailscale.com/kb/1223/funnel)).
- **portless routes** ✗ → portless isn't running. `npm i -g portless && portless proxy start`.

To run the proxy anyway (without the Funnel), use `ptp start --no-funnel`.

## "404 - registered services" for a host I expect to work

The first path segment must be the **exact** portless hostname, including `.local`.
Check `ptp list` for the canonical names. The trailing slash matters for some
apps: prefer `/<host>.local/` over `/<host>.local`.

## `502` upstream error

The dev server that was registered isn't accepting connections (it crashed, or
exited between polls). The 502 body names the `host:port` that failed. Restart the
dev server; `ptp` will pick it up on the next refresh (`--interval`, default 20s).
Lower the interval with `--interval 5` if you want faster pickup.

## Funnel still on after the process died

Fixed in current versions — `ptp` resets the Funnel synchronously on `Ctrl-C`.
If a Funnel is ever left over (e.g. after `kill -9`), clear it with:

```bash
ptp reset          # or: tailscale funnel reset
tailscale serve status   # should print "No serve config"
```

## Port already in use

`ptp` listens on `--port` (default `8443`). If something else holds it:

```bash
ptp start --port 9000
```

## Background mode

`ptp start --bg` detaches and logs to `./ptp.log`. To stop it, find the pid in the
startup message (or `pgrep -f "ptp start"`), `kill` it, then `ptp reset` to be sure
the Funnel is down.
