# Releasing

Releases are **tag-driven**. Pushing a `v*` tag runs
[`.github/workflows/release.yml`](../.github/workflows/release.yml), which:

1. Runs [goreleaser](https://goreleaser.com) to cross-compile all six targets,
   create the GitHub Release with archives + checksums, and update the Homebrew
   cask in `meabed/homebrew-tap`.
2. Generates the per-platform npm packages from the release archives
   (`npm/build-platform-packages.mjs`) and publishes them.
3. Publishes the npm launcher package (`portless-tailscale-proxy`) with its
   `optionalDependencies` pinned to the release version.

## One-time prerequisites

Before the first release, set these up:

| What | Where | Notes |
| --- | --- | --- |
| `NPM_TOKEN` | repo secret | npm **automation** token with publish rights |
| `HOMEBREW_TAP_GITHUB_TOKEN` | repo secret | a PAT (classic, `repo` scope, or fine-grained with contents:write) for the tap repo |
| `meabed/homebrew-tap` | a repo | empty public repo; goreleaser writes `Casks/ptp.rb` |

`GITHUB_TOKEN` is provided automatically by Actions.

Add secrets with:

```bash
gh secret set NPM_TOKEN --repo meabed/portless-tailscale-proxy
gh secret set HOMEBREW_TAP_GITHUB_TOKEN --repo meabed/portless-tailscale-proxy
gh repo create meabed/homebrew-tap --public --description "Homebrew tap"
```

## Cut a release

```bash
git tag v0.1.0
git push origin v0.1.0
```

Watch it:

```bash
gh run watch --repo meabed/portless-tailscale-proxy
gh release view v0.1.0 --repo meabed/portless-tailscale-proxy
npm view portless-tailscale-proxy version
```

## Test the build locally first

No tag, no publish — just prove the matrix compiles and archives:

```bash
goreleaser release --snapshot --clean --skip=publish,announce
ls dist/                       # archives + checksums
node npm/build-platform-packages.mjs 0.0.0-test
ls npm/dist/                   # per-platform npm packages
```

## Distribution channels (all produced from one tag)

- **npm / npx** — launcher + per-platform binary packages (gated by `os`/`cpu`).
- **GitHub Releases** — `tar.gz` (Unix) / `zip` (Windows) archives + `checksums.txt`.
- **Homebrew** — `brew install meabed/tap/ptp` (cask, auto-bumped).
- **curl | sh** — [`install.sh`](../install.sh) fetches the matching release binary.
- **go install** — `go install github.com/meabed/portless-tailscale-proxy@latest`.
