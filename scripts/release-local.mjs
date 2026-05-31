#!/usr/bin/env node
/**
 * Local release helper: build binaries → generate npm packages → publish to npm.
 * For when CI is unavailable or you want a one-off (alpha/beta, hotfix).
 *
 * Usage:
 *   node scripts/release-local.mjs <version> [--dry-run] [--tag <dist-tag>]
 *
 * Prerequisites:
 *   1. `npm whoami`  — must show your npm user (else `npm login`).
 *   2. `goreleaser`, `go`, `node` on PATH.
 *
 * Provenance is disabled here — npm only accepts it from supported CI. CI
 * (semantic-release) keeps publishing the root launcher with provenance.
 */
import { spawnSync } from "node:child_process";

const args = process.argv.slice(2);
const dryRun = args.includes("--dry-run");
const tagIdx = args.indexOf("--tag");
const distTag = tagIdx >= 0 ? args[tagIdx + 1] : null;
// First positional arg that isn't a flag or the --tag value.
const positional = [];
for (let i = 0; i < args.length; i++) {
  if (args[i] === "--tag") { i++; continue; }
  if (args[i].startsWith("--")) continue;
  positional.push(args[i]);
}
const version = positional[0];

if (!version || !/^\d+\.\d+\.\d+(-[\w.]+)?$/.test(version)) {
  console.error("usage: node scripts/release-local.mjs <semver> [--dry-run] [--tag <dist-tag>]");
  process.exit(1);
}

function run(cmd, runArgs, opts = {}) {
  console.log(`\n→ ${cmd} ${runArgs.join(" ")}`);
  const res = spawnSync(cmd, runArgs, { stdio: "inherit", ...opts });
  if (res.status !== 0) {
    console.error(`\n✗ ${cmd} ${runArgs.join(" ")} exited with ${res.status}`);
    process.exit(res.status ?? 1);
  }
}

if (!dryRun) {
  const who = spawnSync("npm", ["whoami"], { stdio: "pipe" });
  if (who.status !== 0) {
    console.error("✗ `npm whoami` failed — run `npm login` first.");
    process.exit(1);
  }
  console.log(`✓ npm user: ${who.stdout.toString().trim()}`);
}

console.log(`\n=== Local release: v${version}${dryRun ? " (dry run)" : ""}${distTag ? ` [tag=${distTag}]` : ""} ===`);

// 1. Build cross-platform binaries + archives (no GitHub publish).
run("env", [`GORELEASER_CURRENT_TAG=v${version}`, "goreleaser", "release",
  "--clean", "--skip=publish,validate,announce"]);

// 2. Generate per-platform npm packages from the archives.
run("node", ["npm/build-platform-packages.mjs", version]);

// 3. Stamp the launcher package version + optionalDependencies.
run("node", ["scripts/stamp-version.mjs", version]);

if (dryRun) {
  console.log("\nDry run — skipping npm publish. Generated npm/dist + dist/.");
  process.exit(0);
}

// 4. Publish platform packages, then the launcher (provenance off for local).
run("node", ["scripts/publish-platform-packages.mjs", version]);
const pub = ["publish", "--access", "public", "--provenance=false"];
if (distTag) pub.push("--tag", distTag);
run("npm", pub);
console.log(`\n✓ published tailscale-proxy@${version}`);
