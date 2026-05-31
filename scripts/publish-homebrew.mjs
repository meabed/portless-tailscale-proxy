#!/usr/bin/env node
// Push the goreleaser-generated Homebrew cask to the tap repo. Best-effort:
// skips cleanly if HOMEBREW_TAP_GITHUB_TOKEN is not set (e.g. local releases).
// goreleaser (run with --skip=publish) writes dist/homebrew/Casks/tsp.rb, whose
// download URLs/sha256 reference the release assets uploaded by semantic-release.
// Usage: node scripts/publish-homebrew.mjs <version>
import { existsSync, mkdtempSync, copyFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { spawnSync } from "node:child_process";

const version = process.argv[2] ?? "";
const token = process.env.HOMEBREW_TAP_GITHUB_TOKEN;
const TAP = "meabed/homebrew-tap";

const caskSrc = join("dist", "homebrew", "Casks", "tsp.rb");
if (!existsSync(caskSrc)) {
  console.log(`no cask at ${caskSrc} — skipping Homebrew publish`);
  process.exit(0);
}
if (!token) {
  console.log("HOMEBREW_TAP_GITHUB_TOKEN unset — skipping Homebrew publish");
  process.exit(0);
}

function run(cmd, args, opts = {}) {
  const res = spawnSync(cmd, args, { stdio: "inherit", ...opts });
  if (res.status !== 0) {
    console.error(`✗ ${cmd} ${args.join(" ")} exited ${res.status}`);
    process.exit(res.status ?? 1);
  }
}

const work = mkdtempSync(join(tmpdir(), "tsp-tap-"));
const repoURL = `https://x-access-token:${token}@github.com/${TAP}.git`;
run("git", ["clone", "--depth", "1", repoURL, work]);
mkdirSync(join(work, "Casks"), { recursive: true });
copyFileSync(caskSrc, join(work, "Casks", "tsp.rb"));
run("git", ["-C", work, "add", "Casks/tsp.rb"]);
run("git", ["-C", work, "-c", "user.name=meabed-bot", "-c", "user.email=mo@meabed.com",
  "commit", "-m", `tsp ${version}`]);
run("git", ["-C", work, "push", "origin", "HEAD"]);
console.log(`pushed Casks/tsp.rb (${version}) to ${TAP}`);
