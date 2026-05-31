#!/usr/bin/env node
"use strict";

const { spawnSync } = require("node:child_process");

// Map Node's platform/arch to our per-platform package + binary name.
function resolveBinary() {
  const platform = process.platform; // 'darwin' | 'linux' | 'win32'
  const arch = process.arch; // 'x64' | 'arm64'
  const pkg = `tailscale-proxy-${platform}-${arch}`;
  const exe = platform === "win32" ? "tsp.exe" : "tsp";
  try {
    return require.resolve(`${pkg}/bin/${exe}`);
  } catch {
    return null;
  }
}

const bin = resolveBinary();
if (!bin) {
  console.error(
    `tailscale-proxy: no prebuilt binary for ${process.platform}-${process.arch}.\n` +
      `Install from source: go install github.com/meabed/tailscale-proxy@latest\n` +
      `or download a release: https://github.com/meabed/tailscale-proxy/releases`
  );
  process.exit(1);
}

const res = spawnSync(bin, process.argv.slice(2), { stdio: "inherit" });
if (res.error) {
  console.error(res.error.message);
  process.exit(1);
}
process.exit(res.status === null ? 1 : res.status);
