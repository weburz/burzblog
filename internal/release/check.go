package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// maxBody caps the answer that is read; a release description is a few KiB.
const maxBody = 1 << 20

// Release is a published BurzBlog release.
type Release struct {
	Tag         string
	URL         string
	PublishedAt time.Time
}

// FetchLatest asks url for the latest release. The request carries only a GET and
// the User-Agent; the answer is read up to a fixed size.
func FetchLatest(ctx context.Context, client *http.Client, url, userAgent string) (Release, error) {
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

// Newer reports whether latest is a newer semantic version than current, with
// or without a leading "v". Anything else (a "dev" build, "test") is never
// newer and never older, so such builds are not told to upgrade.
func Newer(current, latest string) bool {
	c, err := ParseVersion("v" + Trim(current))
	if err != nil {
		return false
	}
	l, err := ParseVersion("v" + Trim(latest))
	if err != nil {
		return false
	}
	return Compare(l, c) > 0
}

// Trim drops a leading "v" so versions print the same however they were tagged.
func Trim(v string) string {
	return strings.TrimPrefix(v, "v")
}
