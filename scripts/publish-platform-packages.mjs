#!/usr/bin/env node
// Publish each per-platform binary package under npm/dist/. These are generated
// by npm/build-platform-packages.mjs from the goreleaser archives.
// Usage: node scripts/publish-platform-packages.mjs <version>
import { readdirSync, existsSync } from "node:fs";
import { join } from "node:path";
import { spawnSync } from "node:child_process";

const version = process.argv[2] ?? "";
const distRoot = join("npm", "dist");
if (!existsSync(distRoot)) {
  console.error(`missing ${distRoot} — run npm/build-platform-packages.mjs first`);
  process.exit(1);
}

// npm provenance only works in supported CI with an OIDC id-token.
const provenance = !!process.env.GITHUB_ACTIONS;

const pkgs = readdirSync(distRoot, { withFileTypes: true })
  .filter((d) => d.isDirectory())
  .map((d) => d.name);

if (pkgs.length === 0) {
  console.error(`no platform packages found in ${distRoot}`);
  process.exit(1);
}

for (const name of pkgs) {
  const dir = join(distRoot, name);
  const args = ["publish", "--access", "public"];
  if (provenance) args.push("--provenance");
  console.log(`\n→ (${name}) npm ${args.join(" ")}  ${version}`);
  const res = spawnSync("npm", args, { stdio: "inherit", cwd: dir });
  if (res.status !== 0) {
    // Treat "already published" as non-fatal so re-runs are idempotent.
    console.warn(`! npm publish for ${name} exited ${res.status} (continuing)`);
  }
}
console.log(`\npublished ${pkgs.length} platform package(s)`);
