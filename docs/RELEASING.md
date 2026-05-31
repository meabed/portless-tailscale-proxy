# Releasing

Releases are **tag-driven**. Pushing a `v*` tag runs
[`.github/workflows/release.yml`](../.github/workflows/release.yml), which:

1. Runs [goreleaser](https://goreleaser.com) to cross-compile all six targets,
   create the GitHub Release with archives + checksums, and update the Homebrew
   cask in `meabed/homebrew-tap`.
2. Generates the per-platform npm packages from the release archives
   (`npm/build-platform-packages.mjs`) and publishes them.
3. Publishes the npm launcher package (`tailscale-proxy`) with its
   `optionalDependencies` pinned to the release version.

Distribution channels: **npx / npm** and **Homebrew** (both pull from the GitHub
Release artifacts).

## One-time prerequisites

| What | Where | Notes |
| --- | --- | --- |
| `NPM_TOKEN` | repo secret | npm **automation** token with publish rights |
| `HOMEBREW_TAP_GITHUB_TOKEN` | repo secret | PAT with contents:write for the tap repo |
| `meabed/homebrew-tap` | a repo | empty public repo; goreleaser writes `Casks/tsp.rb` |

`GITHUB_TOKEN` is provided automatically by Actions.

```bash
gh secret set NPM_TOKEN --repo meabed/tailscale-proxy
gh secret set HOMEBREW_TAP_GITHUB_TOKEN --repo meabed/tailscale-proxy
gh repo create meabed/homebrew-tap --public --description "Homebrew tap"
```

## Cut a release

```bash
git tag v0.1.0
git push origin v0.1.0
```

Watch it:

```bash
gh run watch --repo meabed/tailscale-proxy
gh release view v0.1.0 --repo meabed/tailscale-proxy
npm view tailscale-proxy version
```

## Test the build locally first

No tag, no publish — just prove the matrix compiles and archives:

```bash
goreleaser release --snapshot --clean --skip=publish,announce
ls dist/                       # archives + checksums
node npm/build-platform-packages.mjs 0.0.0-test
ls npm/dist/                   # per-platform npm packages
```

After a release, users update with **`tsp update`** (self-update for standalone
binaries; prints `brew upgrade tsp` / `npm i -g tailscale-proxy@latest` for managed
installs).
