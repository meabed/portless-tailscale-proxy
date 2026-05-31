#!/usr/bin/env node
// Stamp a version into the root launcher package.json: sets `version` and pins
// every optionalDependency (the per-platform binary packages) to that version.
// Usage: node scripts/stamp-version.mjs <version>
import { readFileSync, writeFileSync } from "node:fs";

const version = process.argv[2];
if (!version || !/^\d+\.\d+\.\d+(-[\w.]+)?$/.test(version)) {
  console.error("usage: node scripts/stamp-version.mjs <semver>");
  process.exit(1);
}

const path = "package.json";
const pkg = JSON.parse(readFileSync(path, "utf8"));
pkg.version = version;
for (const dep of Object.keys(pkg.optionalDependencies ?? {})) {
  pkg.optionalDependencies[dep] = version;
}
writeFileSync(path, JSON.stringify(pkg, null, 2) + "\n");
console.log(`stamped ${path} → ${version} (optionalDependencies pinned)`);
