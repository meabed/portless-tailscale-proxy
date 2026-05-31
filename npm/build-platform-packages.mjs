// Generates npm/dist/<pkg>/ for each platform from binaries in dist/.
// Expects goreleaser/CI to have produced binaries at:
//   dist/ptp_<os>_<arch>/ptp[.exe]
// Usage: node npm/build-platform-packages.mjs <version>
import { mkdirSync, writeFileSync, copyFileSync, existsSync } from "node:fs";
import { join } from "node:path";

const version = process.argv[2];
if (!version) {
  console.error("usage: node npm/build-platform-packages.mjs <version>");
  process.exit(1);
}

// [npmPlatform, npmArch, goBinDir, exe]
const targets = [
  ["darwin", "arm64", "ptp_darwin_arm64", "ptp"],
  ["darwin", "x64", "ptp_darwin_amd64_v1", "ptp"],
  ["linux", "x64", "ptp_linux_amd64_v1", "ptp"],
  ["linux", "arm64", "ptp_linux_arm64", "ptp"],
  ["win32", "x64", "ptp_windows_amd64_v1", "ptp.exe"],
  ["win32", "arm64", "ptp_windows_arm64", "ptp.exe"],
];

for (const [os, arch, goDir, exe] of targets) {
  const pkgName = `portless-tailscale-proxy-${os}-${arch}`;
  const outDir = join("npm", "dist", pkgName);
  const binDir = join(outDir, "bin");
  mkdirSync(binDir, { recursive: true });

  const src = join("dist", goDir, exe);
  if (!existsSync(src)) {
    console.error(`missing binary: ${src}`);
    process.exit(1);
  }
  copyFileSync(src, join(binDir, exe));

  const pkg = {
    name: pkgName,
    version,
    description: `Prebuilt portless-tailscale-proxy binary for ${os}-${arch}.`,
    os: [os],
    cpu: [arch],
    license: "MIT",
    repository: {
      type: "git",
      url: "git+https://github.com/meabed/portless-tailscale-proxy.git",
    },
    files: [`bin/${exe}`],
  };
  writeFileSync(join(outDir, "package.json"), JSON.stringify(pkg, null, 2) + "\n");
  console.log(`prepared ${pkgName}@${version}`);
}
