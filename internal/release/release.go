// Package release looks up the newest published BurzBlog release on GitHub and
// compares versions.
package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// LatestURL is the only address BurzBlog contacts to learn about releases.
const LatestURL = "https://api.github.com/repos/weburz/burzblog/releases/latest"

// maxBody caps the answer that is read; a release description is a few KiB.
const maxBody = 1 << 20

// Release is a published BurzBlog release.
type Release struct {
	Tag         string
	URL         string
	PublishedAt time.Time
}

// Latest returns the tag of the newest release, such as "v1.2.0".
func Latest(ctx context.Context, client *http.Client) (string, error) {
	rel, err := Fetch(ctx, client, LatestURL, "burzblog")
	if err != nil {
		return "", err
	}
	return rel.Tag, nil
}

// Fetch asks url for the latest release. The request carries only a GET and
// the User-Agent; the answer is read up to a fixed size.
func Fetch(ctx context.Context, client *http.Client, url, userAgent string) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, fmt.Errorf("build the release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("fetch the latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return Release{}, fmt.Errorf("fetch the latest release: the server answered HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return Release{}, fmt.Errorf("read the latest release: %w", err)
	}
	if len(body) > maxBody {
		return Release{}, errors.New("read the latest release: the answer is larger than 1 MiB")
	}
	var raw struct {
		Tag         string    `json:"tag_name"`
		URL         string    `json:"html_url"`
		PublishedAt time.Time `json:"published_at"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Release{}, fmt.Errorf("decode the latest release: %w", err)
	}
	if raw.Tag == "" {
		return Release{}, errors.New("decode the latest release: it has no tag")
	}
	return Release{Tag: raw.Tag, URL: raw.URL, PublishedAt: raw.PublishedAt}, nil
}

// version is a parsed semantic version.
type version struct {
	core [3]int
	pre  string
}

func parse(s string) (version, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	s, _, _ = strings.Cut(s, "+")
	s, pre, _ := strings.Cut(s, "-")
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return version{}, false
	}
	v := version{pre: pre}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p != strconv.Itoa(n) {
			return version{}, false
		}
		v.core[i] = n
	}
	return v, true
}

// Newer reports whether latest is a newer semantic version than current.
// Anything that is not MAJOR.MINOR.PATCH (a "dev" build, "test") is never
// newer and never older, so such builds are not told to upgrade.
func Newer(current, latest string) bool {
	c, ok := parse(current)
	if !ok {
		return false
	}
	l, ok := parse(latest)
	if !ok {
		return false
	}
	for i := range c.core {
		if l.core[i] != c.core[i] {
			return l.core[i] > c.core[i]
		}
	}
	// Same numbers: a pre-release is older than the release it leads to.
	return c.pre != "" && l.pre == ""
}

// Trim drops a leading "v" so versions print the same however they were tagged.
func Trim(v string) string {
	return strings.TrimPrefix(v, "v")
}
