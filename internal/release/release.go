// Package release finds BurzBlog releases on GitHub and fetches and checks
// their files. It never runs anything it downloads.
//
// The repository's releases follow the layout site/install.sh relies on:
// burzblog-<version>-<os>-<arch> for each platform and SHA256SUMS-<version>
// listing their SHA-256 sums, both under <DownloadBase>/<version>/.
package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Where releases live. Tests and mirrors can point the functions that take a
// URL somewhere else.
const (
	LatestURL    = "https://api.github.com/repos/weburz/burzblog/releases/latest"
	DownloadBase = "https://github.com/weburz/burzblog/releases/download"
)

// Size limits for what is read from the network.
const (
	MaxBinaryBytes = 200 << 20
	MaxSumsBytes   = 1 << 20
	maxAPIBytes    = 4 << 20
)

// NewClient returns an HTTP client for release downloads. It follows
// redirects only to https URLs (or loopback http, see CheckURL).
func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return CheckURL(req.URL.String())
		},
	}
}

// CheckURL accepts https URLs without credentials. Plain http is accepted
// only for 127.0.0.1, ::1 and localhost, so tests can use a local server.
func CheckURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	if u.User != nil {
		return fmt.Errorf("URL %q must not contain credentials", raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		switch u.Hostname() {
		case "127.0.0.1", "::1", "localhost":
			return nil
		}
	}
	return fmt.Errorf("refusing to download from %q: only https is allowed", raw)
}

// Latest returns the tag of the newest release, such as "v1.4.0", from
// LatestURL. A nil client means NewClient with a 30 second timeout.
func Latest(ctx context.Context, client *http.Client) (string, error) {
	return LatestAt(ctx, client, LatestURL)
}

// LatestAt is Latest against an explicit "latest release" API URL.
func LatestAt(ctx context.Context, client *http.Client, apiURL string) (string, error) {
	if err := CheckURL(apiURL); err != nil {
		return "", err
	}
	if client == nil {
		client = NewClient(30 * time.Second)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("look up the latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("look up the latest release: %s answered HTTP %d", apiURL, resp.StatusCode)
	}
	var doc struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAPIBytes)).Decode(&doc); err != nil {
		return "", fmt.Errorf("read the latest release: %w", err)
	}
	if _, err := ParseVersion(doc.TagName); err != nil {
		return "", fmt.Errorf("the latest release has an unusable tag: %w", err)
	}
	return doc.TagName, nil
}

// AssetName is the file name of the binary for a platform, such as
// "burzblog-v1.4.0-linux-amd64".
func AssetName(version, goos, goarch string) string {
	name := "burzblog-" + version + "-" + goos + "-" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// ChecksumName is the file name of a release's checksum list.
func ChecksumName(version string) string {
	return "SHA256SUMS-" + version
}

// AssetURL is where a release file is downloaded from.
func AssetURL(base, version, name string) string {
	return strings.TrimRight(base, "/") + "/" + version + "/" + name
}

// Checksum finds the SHA-256 sum of asset, as lowercase hex, in the text of
// a SHA256SUMS file ("<hex>  <name>" or "<hex> *<name>" per line).
func Checksum(sums []byte, asset string) (string, error) {
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != asset {
			continue
		}
		sum := strings.ToLower(fields[0])
		if raw, err := hex.DecodeString(sum); err != nil || len(raw) != sha256.Size {
			return "", fmt.Errorf("the checksum listed for %s is not a SHA-256 sum", asset)
		}
		return sum, nil
	}
	return "", fmt.Errorf("%s is not listed in the checksum file; this release has no build for your platform", asset)
}

// Fetch downloads rawURL into w, which sees at most limit bytes, and returns
// the SHA-256 sum of what was written as lowercase hex. A larger download is
// an error.
func Fetch(ctx context.Context, client *http.Client, rawURL string, limit int64, w io.Writer) (string, error) {
	if err := CheckURL(rawURL); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: HTTP %d", rawURL, resp.StatusCode)
	}
	if resp.ContentLength > limit {
		return "", fmt.Errorf("download %s: %d bytes is over the %d byte limit", rawURL, resp.ContentLength, limit)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return "", fmt.Errorf("download %s: %w", rawURL, err)
	}
	if n > limit {
		return "", fmt.Errorf("download %s: over the %d byte limit", rawURL, limit)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
