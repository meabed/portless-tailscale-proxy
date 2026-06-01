package core

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const repoSlug = "meabed/tailscale-proxy"

// cmdUpdate updates tsp to the latest release, or prints the right command for
// Homebrew/npm installs.
func cmdUpdate(argv []string) int {
	latest, err := latestVersion()
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not check latest version: %v\n", err)
		return 1
	}
	fmt.Printf("current: %s\nlatest:  %s\n", Version, latest)
	if Version != "dev" && normalizeVer(latest) == normalizeVer(Version) {
		fmt.Println("already up to date.")
		return 0
	}

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot locate executable: %v\n", err)
		return 1
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	switch installMethod(exe) {
	case "brew":
		fmt.Println("\nInstalled via Homebrew — update with:\n  brew upgrade tsp")
		return 0
	case "npm":
		fmt.Println("\nInstalled via npm — update with:\n  npm i -g tailscale-proxy@latest")
		return 0
	default:
		fmt.Printf("\nDownloading %s …\n", latest)
		if err := selfReplace(exe, latest); err != nil {
			fmt.Fprintf(os.Stderr, "self-update failed: %v\n", err)
			return 1
		}
		fmt.Printf("updated to %s\n", latest)
		return 0
	}
}

// installMethod classifies how the binary at path was installed.
func installMethod(path string) string {
	p := strings.ToLower(path)
	if strings.Contains(p, "/cellar/") || strings.Contains(p, "/caskroom/") || strings.Contains(p, "/homebrew/") {
		return "brew"
	}
	if strings.Contains(p, "/node_modules/") || strings.Contains(p, "/_npx/") {
		return "npm"
	}
	return "standalone"
}

// normalizeVer strips a leading v and surrounding whitespace.
func normalizeVer(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

// releaseArchiveURL builds the GitHub release archive URL for an os/arch.
func releaseArchiveURL(tag, goos, goarch string) string {
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/tsp_%s_%s.%s",
		repoSlug, tag, goos, goarch, ext)
}

// latestVersion returns the latest release tag from the GitHub API.
func latestVersion() (string, error) {
	url := "https://api.github.com/repos/" + repoSlug + "/releases/latest"
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "tailscale-proxy-updater")
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GitHub API returned %s", resp.Status)
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	if rel.TagName == "" {
		return "", fmt.Errorf("no releases found")
	}
	return rel.TagName, nil
}

// selfReplace downloads the release binary and atomically replaces exe.
func selfReplace(exe, tag string) error {
	url := releaseArchiveURL(tag, runtime.GOOS, runtime.GOARCH)
	binName := "tsp"
	if runtime.GOOS == "windows" {
		binName = "tsp.exe"
	}

	tmp, err := os.CreateTemp(filepath.Dir(exe), ".tsp-dl-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := downloadBinary(url, binName, tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		old := exe + ".old"
		_ = os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			return err
		}
		return os.Rename(tmpName, exe)
	}
	return os.Rename(tmpName, exe) // replaces the running binary on Unix
}

// downloadBinary fetches url and writes the named binary entry into out.
func downloadBinary(url, binName string, out *os.File) error {
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download %s returned %s", url, resp.Status)
	}
	if strings.HasSuffix(url, ".zip") {
		return extractZipEntry(resp.Body, binName, out)
	}
	return extractTarGzEntry(resp.Body, binName, out)
}

// extractTarGzEntry copies the binName file out of a .tar.gz stream.
func extractTarGzEntry(r io.Reader, binName string, out io.Writer) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("%s not found in archive", binName)
		}
		if err != nil {
			return err
		}
		if filepath.Base(h.Name) == binName {
			_, err := io.Copy(out, tr) //nolint:gosec // trusted GitHub release archive
			return err
		}
	}
}

// extractZipEntry copies the binName file out of a .zip stream (buffered to temp).
func extractZipEntry(r io.Reader, binName string, out io.Writer) error {
	tmp, err := os.CreateTemp("", "tsp-zip-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	n, err := io.Copy(tmp, r)
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(tmp, n)
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		if filepath.Base(f.Name) == binName {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			defer rc.Close()
			_, err = io.Copy(out, rc) //nolint:gosec // trusted GitHub release archive
			return err
		}
	}
	return fmt.Errorf("%s not found in archive", binName)
}
