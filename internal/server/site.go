package server

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/weburz/burzblog/internal/post"
)

// DefaultPostPath is where a post lives on the website unless configured
// otherwise; it matches the Nuxt Content layout in docs/publishing.md.
const DefaultPostPath = "/blog/{slug}"

const (
	maxSiteTitle       = 200
	maxSiteDescription = 1000
)

var languagePattern = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)

// site is the validated description of the public website. Feeds and the
// sitemap use it to build absolute URLs; it is nil when no site URL is set.
type site struct {
	url         string // https://example.com, no trailing slash
	title       string
	description string
	language    string
	postPath    string // contains {slug}
	tagPath     string // contains {tag}; empty means no tag pages
}

// newSite validates the site settings of cfg. It returns nil when no site URL
// is set.
func newSite(cfg Config) (*site, error) {
	if cfg.SiteURL == "" {
		return nil, nil
	}
	base, err := ParseOrigin(cfg.SiteURL)
	if err != nil {
		return nil, fmt.Errorf("site URL must look like https://example.com, got %q", cfg.SiteURL)
	}
	st := &site{
		url:         base.String(),
		title:       strings.TrimSpace(cfg.SiteTitle),
		description: strings.TrimSpace(cfg.SiteDescription),
		language:    cfg.SiteLanguage,
		postPath:    cfg.PostPath,
		tagPath:     cfg.TagPath,
	}
	if st.title == "" {
		st.title = base.Host
	}
	if st.description == "" {
		st.description = st.title
	}
	if st.language == "" {
		st.language = "en"
	}
	if st.postPath == "" {
		st.postPath = DefaultPostPath
	}
	for _, f := range []struct {
		name, value string
		max         int
	}{{"site title", st.title, maxSiteTitle}, {"site description", st.description, maxSiteDescription}} {
		if post.HasControl(f.value) || utf8.RuneCountInString(f.value) > f.max {
			return nil, fmt.Errorf("%s must be at most %d characters without control characters", f.name, f.max)
		}
	}
	if !languagePattern.MatchString(st.language) {
		return nil, fmt.Errorf("site language must be a language tag such as en or pt-BR, got %q", st.language)
	}
	if err := checkPattern("post path", st.postPath, "{slug}"); err != nil {
		return nil, err
	}
	if st.tagPath != "" {
		if err := checkPattern("tag path", st.tagPath, "{tag}"); err != nil {
			return nil, err
		}
	}
	return st, nil
}

// checkPattern requires an absolute path with the placeholder exactly once.
func checkPattern(name, pattern, placeholder string) error {
	if !strings.HasPrefix(pattern, "/") || strings.HasPrefix(pattern, "//") || strings.Count(pattern, placeholder) != 1 ||
		strings.ContainsAny(pattern, "?#") || post.HasControl(pattern) {
		return fmt.Errorf("%s must start with / and contain %s once, like /blog/{slug}, got %q", name, placeholder, pattern)
	}
	return nil
}

func (st *site) postURL(slug string) string {
	return st.url + strings.Replace(st.postPath, "{slug}", url.PathEscape(slug), 1)
}

func (st *site) tagURL(tag string) string {
	return st.url + strings.Replace(st.tagPath, "{tag}", url.PathEscape(tag), 1)
}

// siteSettings is the site's part of GET /api/settings.
func (s *Server) siteSettings() map[string]any {
	feeds := []string{}
	if s.site != nil {
		for _, f := range feedFormats {
			feeds = append(feeds, "/api/content/"+f.file)
		}
		feeds = append(feeds, "/api/content/"+sitemapFile)
	}
	out := map[string]any{"configured": s.site != nil, "feeds": feeds}
	if st := s.site; st != nil {
		out["url"], out["title"], out["description"], out["language"] = st.url, st.title, st.description, st.language
		out["postPath"], out["tagPath"] = st.postPath, st.tagPath
	}
	return out
}
