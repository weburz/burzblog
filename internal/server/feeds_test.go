package server

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	trickyTitle  = "Fish & <Chips> ]]> 🎉 café"
	trickyAuthor = "Ada <Lovelace> & Co"
	siteURL      = "https://example.com"
)

func withSite(c *Config) {
	c.SiteURL = siteURL + "/"
	c.SiteTitle = "Example & Sons"
	c.SiteDescription = "Notes <from> the shop"
	c.SiteLanguage = "pt-BR"
}

// newFeedHarness is the content harness plus a published post that is hard on
// XML, a published post the renderer refuses, and a tag on "live".
func newFeedHarness(t *testing.T, mutate func(*Config)) *contentHarness {
	t.Helper()
	c := newContentHarnessWith(t, mutate)
	mk := func(slug string, fields map[string]any) {
		fields["slug"] = slug
		if resp, out := c.do("POST", "/api/posts", fields); resp.StatusCode != 201 {
			t.Fatalf("create %s = %d %v", slug, resp.StatusCode, out)
		}
		if resp, out := c.do("POST", "/api/posts/"+slug+"/publish", map[string]any{"version": 1}); resp.StatusCode != 200 {
			t.Fatalf("publish %s = %d %v", slug, resp.StatusCode, out)
		}
	}
	mk("tricky", map[string]any{
		"title": trickyTitle, "description": "Salt & <vinegar> ]]> 🎉", "author": trickyAuthor,
		"tags": []string{"go", "web-dev"}, "coverUrl": "https://example.com/c.png?a=1&b=2",
		"body": "# Heading <b>\n\nTom & Jerry ]]> 🎉 café <script>alert(1)</script>\n\n```\nif a < b && c > d {}\n```\n",
	})
	wide := "|" + strings.Repeat(" x |", 65) + "\n|" + strings.Repeat("---|", 65) + "\n"
	mk("wide", map[string]any{"title": "Too wide", "description": "A table the renderer refuses.", "body": wide})
	return c
}

type rssDoc struct {
	XMLName xml.Name `xml:"rss"`
	Version string   `xml:"version,attr"`
	Channel struct {
		Title         string `xml:"title"`
		Link          string `xml:"link"`
		Description   string `xml:"description"`
		Language      string `xml:"language"`
		LastBuildDate string `xml:"lastBuildDate"`
		Items         []struct {
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			PubDate     string `xml:"pubDate"`
			Description string `xml:"description"`
			GUID        struct {
				IsPermaLink string `xml:"isPermaLink,attr"`
				Value       string `xml:",chardata"`
			} `xml:"guid"`
			Content    string   `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
			Creator    string   `xml:"http://purl.org/dc/elements/1.1/ creator"`
			Categories []string `xml:"category"`
		} `xml:"item"`
	} `xml:"channel"`
}

type atomDoc struct {
	XMLName xml.Name `xml:"http://www.w3.org/2005/Atom feed"`
	Title   string   `xml:"title"`
	ID      string   `xml:"id"`
	Updated string   `xml:"updated"`
	Entries []struct {
		Title     string `xml:"title"`
		ID        string `xml:"id"`
		Published string `xml:"published"`
		Updated   string `xml:"updated"`
		Summary   string `xml:"summary"`
		Link      struct {
			Href string `xml:"href,attr"`
		} `xml:"link"`
		Author *struct {
			Name string `xml:"name"`
		} `xml:"author"`
		Categories []struct {
			Term string `xml:"term,attr"`
		} `xml:"category"`
		Content *struct {
			Type  string `xml:"type,attr"`
			Value string `xml:",chardata"`
		} `xml:"content"`
	} `xml:"entry"`
}

type jsonDoc struct {
	Version     string `json:"version"`
	Title       string `json:"title"`
	HomePageURL string `json:"home_page_url"`
	Description string `json:"description"`
	Language    string `json:"language"`
	Items       []struct {
		ID            string   `json:"id"`
		URL           string   `json:"url"`
		Title         string   `json:"title"`
		Summary       string   `json:"summary"`
		Image         string   `json:"image"`
		ContentHTML   string   `json:"content_html"`
		ContentText   string   `json:"content_text"`
		DatePublished string   `json:"date_published"`
		DateModified  string   `json:"date_modified"`
		Tags          []string `json:"tags"`
		Authors       []struct {
			Name string `json:"name"`
		} `json:"authors"`
	} `json:"items"`
}

func TestFeedsAreWellFormed(t *testing.T) {
	c := newFeedHarness(t, withSite)
	wantLink := siteURL + "/blog/tricky"

	for _, secret := range []string{c.export, c.write} {
		resp, raw := c.get(secret, "/api/content/feed.xml")
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/rss+xml; charset=utf-8" {
			t.Fatalf("rss = %d %q %s", resp.StatusCode, resp.Header.Get("Content-Type"), raw)
		}
	}

	resp, raw := c.get(c.export, "/api/content/feed.xml")
	var rss rssDoc
	if err := xml.Unmarshal([]byte(raw), &rss); err != nil {
		t.Fatalf("rss does not parse: %v\n%s", err, raw)
	}
	if !strings.HasPrefix(raw, "<?xml") || rss.Version != "2.0" || rss.Channel.Title != "Example & Sons" ||
		rss.Channel.Link != siteURL+"/" || rss.Channel.Description != "Notes <from> the shop" || rss.Channel.Language != "pt-BR" {
		t.Errorf("rss channel = %+v", rss.Channel)
	}
	if _, err := time.Parse(time.RFC1123Z, rss.Channel.LastBuildDate); err != nil {
		t.Errorf("lastBuildDate: %v", err)
	}
	if got := len(rss.Channel.Items); got != 3 {
		t.Fatalf("rss has %d items, want live, tricky and wide only:\n%s", got, raw)
	}
	for _, item := range rss.Channel.Items {
		if strings.Contains(raw, "secret") {
			t.Fatalf("a draft, scheduled or archived post leaked:\n%s", raw)
		}
		if item.GUID.IsPermaLink != "true" || item.GUID.Value != item.Link || !strings.HasPrefix(item.Link, siteURL+"/blog/") {
			t.Errorf("item link/guid = %q %+v", item.Link, item.GUID)
		}
		if _, err := time.Parse(time.RFC1123Z, item.PubDate); err != nil {
			t.Errorf("pubDate %q: %v", item.PubDate, err)
		}
		switch item.Link {
		case wantLink:
			if item.Title != trickyTitle || item.Creator != trickyAuthor || item.Description != "Salt & <vinegar> ]]> 🎉" ||
				!slices.Equal(item.Categories, []string{"go", "web-dev"}) {
				t.Errorf("tricky item = %+v", item)
			}
			for _, want := range []string{"Tom &amp; Jerry ]]&gt; 🎉 café", "a &lt; b &amp;&amp; c &gt; d"} {
				if !strings.Contains(item.Content, want) {
					t.Errorf("content lacks %q:\n%s", want, item.Content)
				}
			}
			if strings.Contains(item.Content, "<script") {
				t.Errorf("content is not sanitized:\n%s", item.Content)
			}
		case siteURL + "/blog/wide":
			if item.Content != "" || item.Description != "A table the renderer refuses." {
				t.Errorf("a post over the render limits must fall back to its description: %+v", item)
			}
		}
	}

	resp, raw = c.get(c.export, "/api/content/feed.atom")
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/atom+xml; charset=utf-8" {
		t.Fatalf("atom = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	var atom atomDoc
	if err := xml.Unmarshal([]byte(raw), &atom); err != nil {
		t.Fatalf("atom does not parse: %v\n%s", err, raw)
	}
	if atom.Title != "Example & Sons" || atom.ID != siteURL+"/" || len(atom.Entries) != 3 || !strings.Contains(raw, `xml:lang="pt-BR"`) {
		t.Errorf("atom = %+v\n%s", atom, raw)
	}
	if _, err := time.Parse(time.RFC3339, atom.Updated); err != nil {
		t.Errorf("atom updated: %v", err)
	}
	for _, e := range atom.Entries {
		for _, stamp := range []string{e.Published, e.Updated} {
			if _, err := time.Parse(time.RFC3339, stamp); err != nil {
				t.Errorf("atom stamp %q: %v", stamp, err)
			}
		}
		if e.ID != e.Link.Href {
			t.Errorf("atom id %q != link %q", e.ID, e.Link.Href)
		}
		switch e.ID {
		case wantLink:
			if e.Title != trickyTitle || e.Author == nil || e.Author.Name != trickyAuthor || len(e.Categories) != 2 ||
				e.Content == nil || e.Content.Type != "html" || !strings.Contains(e.Content.Value, "Tom &amp; Jerry ]]&gt; 🎉 café") {
				t.Errorf("tricky entry = %+v", e)
			}
		case siteURL + "/blog/wide":
			if e.Content != nil || e.Summary == "" {
				t.Errorf("wide entry = %+v", e)
			}
		}
	}

	resp, raw = c.get(c.export, "/api/content/feed.json")
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/feed+json; charset=utf-8" {
		t.Fatalf("json feed = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	var feed jsonDoc
	if err := json.Unmarshal([]byte(raw), &feed); err != nil {
		t.Fatalf("json feed does not parse: %v\n%s", err, raw)
	}
	if feed.Version != "https://jsonfeed.org/version/1.1" || feed.Title != "Example & Sons" || feed.HomePageURL != siteURL+"/" ||
		feed.Language != "pt-BR" || len(feed.Items) != 3 {
		t.Errorf("json feed = %+v", feed)
	}
	for _, item := range feed.Items {
		if item.ContentHTML == "" && item.ContentText == "" {
			t.Errorf("item %s has neither content_html nor content_text", item.ID)
		}
		if item.ID != item.URL {
			t.Errorf("id %q != url %q", item.ID, item.URL)
		}
		if _, err := time.Parse(time.RFC3339, item.DatePublished); err != nil {
			t.Errorf("date_published: %v", err)
		}
		if item.ID == wantLink && (item.Title != trickyTitle || len(item.Authors) != 1 || item.Authors[0].Name != trickyAuthor ||
			item.Image != "https://example.com/c.png?a=1&b=2" || !slices.Equal(item.Tags, []string{"go", "web-dev"})) {
			t.Errorf("tricky json item = %+v", item)
		}
	}
}

func TestFeedLimitAndTag(t *testing.T) {
	c := newFeedHarness(t, withSite)
	count := func(path string) int {
		t.Helper()
		resp, raw := c.get(c.export, path)
		if resp.StatusCode != 200 {
			t.Fatalf("%s = %d %s", path, resp.StatusCode, raw)
		}
		var feed jsonDoc
		if err := json.Unmarshal([]byte(raw), &feed); err != nil {
			t.Fatal(err)
		}
		return len(feed.Items)
	}
	if got := count("/api/content/feed.json?limit=2"); got != 2 {
		t.Errorf("limit=2 gave %d items", got)
	}
	if got := count("/api/content/feed.json?tag=Web%20Dev"); got != 1 {
		t.Errorf("tag filter gave %d items, want only tricky", got)
	}
	if got := count("/api/content/feed.json?tag=nothing"); got != 0 {
		t.Errorf("unknown tag gave %d items", got)
	}
	resp, raw := c.get(c.export, "/api/content/feed.xml?tag=nothing")
	var rss rssDoc
	if resp.StatusCode != 200 || xml.Unmarshal([]byte(raw), &rss) != nil || len(rss.Channel.Items) != 0 {
		t.Errorf("empty rss = %d %s", resp.StatusCode, raw)
	}
	for _, bad := range []string{"0", "101", "x", "-1"} {
		if resp, raw := c.get(c.export, "/api/content/feed.xml?limit="+bad); resp.StatusCode != 422 {
			t.Errorf("limit=%s = %d %s, want 422", bad, resp.StatusCode, raw)
		}
	}
}

func TestFeedsAndSitemapNeedAuthentication(t *testing.T) {
	c := newFeedHarness(t, withSite)
	for _, file := range []string{"feed.xml", "feed.atom", "feed.json", "sitemap.xml"} {
		if resp, _ := c.get("", "/api/content/"+file); resp.StatusCode != 401 {
			t.Errorf("anonymous %s = %d, want 401", file, resp.StatusCode)
		}
		if resp, _ := c.get(c.export, "/api/content/"+file); resp.StatusCode != 200 {
			t.Errorf("export token %s = %d, want 200", file, resp.StatusCode)
		}
	}
}

func TestFeedsAnswer304(t *testing.T) {
	c := newFeedHarness(t, withSite)
	for _, file := range []string{"feed.xml", "feed.atom", "feed.json", "sitemap.xml"} {
		path := "/api/content/" + file
		resp, _ := c.get(c.export, path)
		tag := resp.Header.Get("ETag")
		if tag == "" || resp.Header.Get("Cache-Control") != "private, max-age=60" {
			t.Fatalf("%s headers = %v", file, resp.Header)
		}
		resp, raw := c.get(c.export, path, "If-None-Match", tag)
		if resp.StatusCode != http.StatusNotModified || raw != "" {
			t.Errorf("%s with its ETag = %d %q", file, resp.StatusCode, raw)
		}
		if resp, _ := c.get(c.export, path+"?limit=5", "If-None-Match", tag); file != "sitemap.xml" && resp.StatusCode != 200 {
			t.Errorf("%s ETag reused for another limit = %d", file, resp.StatusCode)
		}
	}

	_, before := c.get(c.export, "/api/content/feed.xml")
	resp, _ := c.get(c.export, "/api/content/feed.xml")
	tag := resp.Header.Get("ETag")
	if r, out := c.do("PUT", "/api/posts/tricky", map[string]any{"version": 2, "title": "Changed", "body": "x"}); r.StatusCode != 200 {
		t.Fatalf("edit = %d %v", r.StatusCode, out)
	}
	resp, after := c.get(c.export, "/api/content/feed.xml", "If-None-Match", tag)
	if resp.StatusCode != 200 || after == before || !strings.Contains(after, "Changed") {
		t.Errorf("an edited post must change the feed: %d", resp.StatusCode)
	}
}

func TestFeedsNeedSiteURL(t *testing.T) {
	c := newFeedHarness(t, nil)
	for _, file := range []string{"feed.xml", "feed.atom", "feed.json", "sitemap.xml"} {
		resp, raw := c.get(c.export, "/api/content/"+file)
		var out map[string]map[string]string
		if resp.StatusCode != 409 || json.Unmarshal([]byte(raw), &out) != nil || out["error"]["code"] != "site_url_missing" ||
			!strings.Contains(out["error"]["message"], "--site-url") {
			t.Errorf("%s without a site URL = %d %s", file, resp.StatusCode, raw)
		}
	}
	req, _ := http.NewRequest("GET", c.ts.URL+"/api/export.tar.gz", nil)
	req.Header.Set("Authorization", "Bearer "+c.export)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	files := readArchive(t, resp.Body)
	if _, ok := files["live.md"]; !ok || len(files) != 3 {
		t.Errorf("export without a site URL holds %d files, want only the 3 posts", len(files))
	}
}

func TestExportHoldsFeedsAndSitemap(t *testing.T) {
	c := newFeedHarness(t, func(cfg *Config) { withSite(cfg); cfg.TagPath = "/tags/{tag}" })
	req, _ := http.NewRequest("GET", c.ts.URL+"/api/export.tar.gz", nil)
	req.Header.Set("Authorization", "Bearer "+c.export)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	files := readArchive(t, resp.Body)
	if len(files) != 7 {
		t.Fatalf("export holds %d files, want 3 posts and 4 site files", len(files))
	}
	for file, endpoint := range map[string]string{"feed.xml": "feed.xml", "feed.atom": "feed.atom", "feed.json": "feed.json", "sitemap.xml": "sitemap.xml"} {
		_, live := c.get(c.export, "/api/content/"+endpoint)
		if files[file] != live {
			t.Errorf("%s in the export differs from the endpoint:\n%s\n%s", file, files[file], live)
		}
	}
}

type sitemapDoc struct {
	XMLName xml.Name `xml:"http://www.sitemaps.org/schemas/sitemap/0.9 urlset"`
	URLs    []struct {
		Loc     string `xml:"loc"`
		LastMod string `xml:"lastmod"`
	} `xml:"url"`
}

func TestSitemap(t *testing.T) {
	c := newFeedHarness(t, func(cfg *Config) {
		withSite(cfg)
		cfg.PostPath = "/notes/{slug}/"
		cfg.TagPath = "/tags/{tag}"
	})
	resp, raw := c.get(c.export, "/api/content/sitemap.xml")
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/xml; charset=utf-8" {
		t.Fatalf("sitemap = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	var set sitemapDoc
	if err := xml.Unmarshal([]byte(raw), &set); err != nil {
		t.Fatalf("sitemap does not parse: %v\n%s", err, raw)
	}
	locs := map[string]string{}
	for _, u := range set.URLs {
		locs[u.Loc] = u.LastMod
	}
	for _, want := range []string{"/notes/live/", "/notes/tricky/", "/notes/wide/"} {
		stamp, ok := locs[siteURL+want]
		if _, err := time.Parse(time.RFC3339, stamp); !ok || err != nil {
			t.Errorf("sitemap lacks %s with a lastmod (%q, %v):\n%s", want, stamp, err, raw)
		}
	}
	for _, want := range []string{"/tags/go", "/tags/web-dev"} {
		if stamp, ok := locs[siteURL+want]; !ok || stamp != "" {
			t.Errorf("sitemap tag page %s = %q, %v", want, stamp, ok)
		}
	}
	if len(set.URLs) != 5 || strings.Contains(raw, "draft") || strings.Contains(raw, "later") || strings.Contains(raw, "/old/") {
		t.Errorf("sitemap has %d urls, want 3 posts and 2 tags:\n%s", len(set.URLs), raw)
	}

	// Without --tag-path there are no tag pages.
	plain := newFeedHarness(t, withSite)
	_, raw = plain.get(plain.export, "/api/content/sitemap.xml")
	if strings.Contains(raw, "/tags/") {
		t.Errorf("sitemap has tag pages without a tag path:\n%s", raw)
	}
}

func TestSitemapStopsAtTheProtocolLimit(t *testing.T) {
	old := maxSitemapURLs
	maxSitemapURLs = 4
	t.Cleanup(func() { maxSitemapURLs = old })
	c := newFeedHarness(t, func(cfg *Config) { withSite(cfg); cfg.TagPath = "/tags/{tag}" })
	_, raw := c.get(c.export, "/api/content/sitemap.xml")
	var set sitemapDoc
	if err := xml.Unmarshal([]byte(raw), &set); err != nil {
		t.Fatal(err)
	}
	// Three posts and the first of two tags fill the four URLs.
	if len(set.URLs) != 4 || set.URLs[3].Loc != siteURL+"/tags/go" {
		t.Errorf("sitemap over the limit = %+v", set.URLs)
	}
	maxSitemapURLs = 2
	_, raw = c.get(c.export, "/api/content/sitemap.xml")
	set = sitemapDoc{}
	if err := xml.Unmarshal([]byte(raw), &set); err != nil || len(set.URLs) != 2 || strings.Contains(raw, "/tags/") {
		t.Errorf("posts must win over tag pages: %v %s", err, raw)
	}
}

func TestSettingsReportTheSite(t *testing.T) {
	c := newFeedHarness(t, withSite)
	_, out := c.do("GET", "/api/settings", nil)
	site, _ := out["site"].(map[string]any)
	if site["configured"] != true || site["url"] != siteURL || site["title"] != "Example & Sons" || site["language"] != "pt-BR" ||
		site["postPath"] != DefaultPostPath || len(site["feeds"].([]any)) != 4 {
		t.Errorf("settings site = %v", site)
	}
	bare := newHarness(t, nil)
	bare.login()
	_, out = bare.do("GET", "/api/settings", nil)
	site, _ = out["site"].(map[string]any)
	if site["configured"] != false || len(site["feeds"].([]any)) != 0 || site["url"] != nil {
		t.Errorf("settings without a site = %v", site)
	}
}

func TestSiteConfigValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
		reject string // substring of the error; empty means accepted
	}{
		{"nothing set", func(c *Config) {}, ""},
		{"defaults", func(c *Config) { c.SiteURL = "https://example.com" }, ""},
		{"full", func(c *Config) {
			c.SiteURL, c.SiteLanguage, c.PostPath, c.TagPath = "http://localhost:3000", "en-GB", "/posts/{slug}", "/t/{tag}"
		}, ""},
		{"url with a path", func(c *Config) { c.SiteURL = "https://example.com/blog" }, "site URL"},
		{"url without a scheme", func(c *Config) { c.SiteURL = "example.com" }, "site URL"},
		{"bad language", func(c *Config) { c.SiteURL, c.SiteLanguage = "https://example.com", "english please" }, "site language"},
		{"post path without slug", func(c *Config) { c.SiteURL, c.PostPath = "https://example.com", "/blog" }, "{slug}"},
		{"post path with two slugs", func(c *Config) { c.SiteURL, c.PostPath = "https://example.com", "/{slug}/{slug}" }, "{slug}"},
		{"relative post path", func(c *Config) { c.SiteURL, c.PostPath = "https://example.com", "blog/{slug}" }, "start with /"},
		{"protocol-relative post path", func(c *Config) { c.SiteURL, c.PostPath = "https://example.com", "//evil.example/{slug}" }, "start with /"},
		{"tag path without tag", func(c *Config) { c.SiteURL, c.TagPath = "https://example.com", "/tags" }, "{tag}"},
		{"title with a control character", func(c *Config) { c.SiteURL, c.SiteTitle = "https://example.com", "a\x1bb" }, "site title"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{Origin: "http://127.0.0.1:8080"}
			tc.mutate(&cfg)
			_, err := newSite(cfg)
			switch {
			case tc.reject == "" && err != nil:
				t.Errorf("rejected: %v", err)
			case tc.reject != "" && (err == nil || !strings.Contains(err.Error(), tc.reject)):
				t.Errorf("error = %v, want mention of %q", err, tc.reject)
			}
		})
	}
	if _, err := New(Config{Origin: "http://127.0.0.1:8080", SiteURL: "nope"}, nil, nil); err == nil {
		t.Error("New accepted a bad site URL")
	}
}
