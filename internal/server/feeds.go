package server

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/weburz/burzblog/internal/post"
	"github.com/weburz/burzblog/internal/render"
	"github.com/weburz/burzblog/internal/store"
)

// Feeds and the sitemap are produced for the website to serve: readers never
// talk to BurzBlog. They sit behind the same reader gate as the rest of the
// content API and are part of the export archive. Every URL in them is built
// from the configured site URL and post path.

const (
	defaultFeedItems = 20
	maxFeedItems     = 100
	sitemapFile      = "sitemap.xml"
)

// maxSitemapURLs is the sitemap protocol's limit on URLs per file. A variable
// so tests can lower it.
var maxSitemapURLs = 50000

// feedEntry is a published post with its rendered body. HTML is empty when the
// post could not be rendered; the feed then carries the description only.
type feedEntry struct {
	Post *post.Post
	HTML string
}

// feedFormat is one syndication format of the same entries.
type feedFormat struct {
	file        string
	contentType string
	encode      func(*site, []feedEntry) ([]byte, error)
}

var feedFormats = []feedFormat{
	{"feed.xml", "application/rss+xml; charset=utf-8", encodeRSS},
	{"feed.atom", "application/atom+xml; charset=utf-8", encodeAtom},
	{"feed.json", "application/feed+json; charset=utf-8", encodeJSONFeed},
}

// modified is when a post last changed for readers: the author's updatedOn,
// else its publication date, else the last save.
func modified(p *post.Post) time.Time {
	switch {
	case p.UpdatedOn != nil:
		return p.UpdatedOn.UTC()
	case p.PublishedOn != nil:
		return p.PublishedOn.UTC()
	}
	return p.UpdatedAt.UTC()
}

func published(p *post.Post) time.Time {
	if p.PublishedOn != nil {
		return p.PublishedOn.UTC()
	}
	return p.UpdatedAt.UTC()
}

// requireSite answers 409 when no site URL is set and reports whether the
// request may continue.
func (s *Server) requireSite(w http.ResponseWriter) bool {
	if s.site != nil {
		return true
	}
	fail(w, http.StatusConflict, "site_url_missing",
		"No site URL is set, so feeds and the sitemap cannot contain absolute links. Start BurzBlog with --site-url https://example.com (or BURZBLOG_SITE_URL).", "")
	return false
}

// feedLimit reads ?limit=, 1 to maxFeedItems.
func feedLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return defaultFeedItems, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > maxFeedItems {
		fail(w, http.StatusUnprocessableEntity, "validation", fmt.Sprintf("Limit must be 1 to %d.", maxFeedItems), "limit")
		return 0, false
	}
	return n, true
}

// digest tags a representation by what it is built from, so a 304 needs no
// rendering. The BurzBlog version is part of it because the renderer may change.
func (s *Server) digest(kind string, parts ...string) string {
	h := sha256.New()
	for _, part := range append([]string{kind, s.cfg.Version, s.site.url, s.site.title, s.site.description,
		s.site.language, s.site.postPath, s.site.tagPath}, parts...) {
		fmt.Fprintf(h, "%d:%s;", len(part), part)
	}
	return `"` + hex.EncodeToString(h.Sum(nil)[:8]) + `"`
}

// listingTag digests the posts of a listing by slug and version.
func (s *Server) listingTag(kind, extra string, list []store.Listing) string {
	parts := []string{extra}
	for _, l := range list {
		parts = append(parts, l.Post.Slug+":"+strconv.FormatInt(l.Post.Version, 10))
	}
	return s.digest(kind, parts...)
}

// feedEntries loads each listed post's body and renders it. A post the
// renderer refuses (too large, too complex, no free render slot) keeps its
// description only, so one bad post does not fail the feed.
func (s *Server) feedEntries(ctx context.Context, list []store.Listing) ([]feedEntry, error) {
	entries := make([]feedEntry, 0, len(list))
	for _, l := range list {
		p, err := s.store.GetPost(ctx, l.Post.Slug)
		if errors.Is(err, store.ErrNotFound) || (err == nil && p.Status != post.Published) {
			continue // unpublished since the listing
		}
		if err != nil {
			return nil, err
		}
		html, err := render.HTML(ctx, p.Body)
		var limit *render.LimitError
		switch {
		case errors.As(err, &limit) || errors.Is(err, render.ErrBusy):
			s.log.Warn("feed entry falls back to its description", "slug", p.Slug, "err", err)
			html = ""
		case err != nil:
			return nil, err
		}
		entries = append(entries, feedEntry{Post: p, HTML: html})
	}
	return entries, nil
}

// contentFeed serves one feed format: the newest published posts, optionally
// of one tag.
func (s *Server) contentFeed(f feedFormat) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.requireSite(w) {
			return
		}
		limit, ok := feedLimit(w, r)
		if !ok {
			return
		}
		tag := r.URL.Query().Get("tag")
		list, _, err := s.store.ListPublished(r.Context(), "", tag, limit, 0)
		if err != nil {
			s.internal(w, r, err)
			return
		}
		etag := s.listingTag(f.file, strconv.Itoa(limit)+"|"+tag, list)
		if notModified(w, r, etag) {
			return
		}
		entries, err := s.feedEntries(r.Context(), list)
		if err == nil {
			var out []byte
			if out, err = f.encode(s.site, entries); err == nil {
				cached(w, etag)
				w.Header().Set("Content-Type", f.contentType)
				_, _ = w.Write(out)
				return
			}
		}
		s.internal(w, r, err)
	}
}

// contentSitemap serves the sitemap of every published post.
func (s *Server) contentSitemap(w http.ResponseWriter, r *http.Request) {
	if !s.requireSite(w) {
		return
	}
	list, tags, total, err := s.sitemapSources(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	etag := s.listingTag(sitemapFile, fmt.Sprint(total, maxSitemapURLs, len(tags)), list)
	if notModified(w, r, etag) {
		return
	}
	out, err := encodeSitemap(s.site, list, tags)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	cached(w, etag)
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write(out)
}

// sitemapSources lists the posts and tags a sitemap holds. The file may hold
// maxSitemapURLs URLs: posts come first, newest published first, and tag pages
// only fill what is left.
func (s *Server) sitemapSources(ctx context.Context) (list []store.Listing, tags []store.TagCount, total int, err error) {
	list, total, err = s.store.ListPublished(ctx, "", "", maxSitemapURLs, 0)
	if err != nil {
		return nil, nil, 0, err
	}
	if total > len(list) {
		s.log.Warn("sitemap holds only the newest posts", "limit", maxSitemapURLs, "posts", total)
	}
	if s.site.tagPath != "" {
		if tags, err = s.store.PublishedTags(ctx); err != nil {
			return nil, nil, 0, err
		}
		if room := max(maxSitemapURLs-len(list), 0); len(tags) > room {
			tags = tags[:room]
		}
	}
	return list, tags, total, nil
}

// siteFiles generates the feeds and the sitemap for the export archive, keyed
// by file name. It is empty when no site URL is set. It runs only short
// queries, one at a time, so no database connection is held while a slow
// client downloads.
func (s *Server) siteFiles(ctx context.Context) (map[string][]byte, error) {
	files := map[string][]byte{}
	if s.site == nil {
		return files, nil
	}
	list, _, err := s.store.ListPublished(ctx, "", "", defaultFeedItems, 0)
	if err != nil {
		return nil, err
	}
	entries, err := s.feedEntries(ctx, list)
	if err != nil {
		return nil, err
	}
	for _, f := range feedFormats {
		if files[f.file], err = f.encode(s.site, entries); err != nil {
			return nil, err
		}
	}
	posts, tags, _, err := s.sitemapSources(ctx)
	if err != nil {
		return nil, err
	}
	if files[sitemapFile], err = encodeSitemap(s.site, posts, tags); err != nil {
		return nil, err
	}
	return files, nil
}

func marshalXML(v any) ([]byte, error) {
	body, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode xml: %w", err)
	}
	return append([]byte(xml.Header), append(body, '\n')...), nil
}

// RSS 2.0, with the content and Dublin Core modules for the full body and the
// author's name (RSS's own author element must be an email address).

type rssFeed struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Content string     `xml:"xmlns:content,attr"`
	DC      string     `xml:"xmlns:dc,attr"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title         string    `xml:"title"`
	Link          string    `xml:"link"`
	Description   string    `xml:"description"`
	Language      string    `xml:"language"`
	LastBuildDate string    `xml:"lastBuildDate"`
	Generator     string    `xml:"generator"`
	Items         []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string   `xml:"title"`
	Link        string   `xml:"link"`
	GUID        rssGUID  `xml:"guid"`
	PubDate     string   `xml:"pubDate"`
	Description string   `xml:"description,omitempty"`
	Content     string   `xml:"content:encoded,omitempty"`
	Creator     string   `xml:"dc:creator,omitempty"`
	Categories  []string `xml:"category"`
}

type rssGUID struct {
	IsPermaLink bool   `xml:"isPermaLink,attr"`
	Value       string `xml:",chardata"`
}

func encodeRSS(st *site, entries []feedEntry) ([]byte, error) {
	feed := rssFeed{
		Version: "2.0", Content: "http://purl.org/rss/1.0/modules/content/", DC: "http://purl.org/dc/elements/1.1/",
		Channel: rssChannel{
			Title: st.title, Link: st.url + "/", Description: st.description, Language: st.language,
			LastBuildDate: feedUpdated(entries).Format(time.RFC1123Z), Generator: "BurzBlog",
		},
	}
	for _, e := range entries {
		link := st.postURL(e.Post.Slug)
		feed.Channel.Items = append(feed.Channel.Items, rssItem{
			Title: e.Post.Title, Link: link, GUID: rssGUID{true, link},
			PubDate: published(e.Post).Format(time.RFC1123Z), Description: e.Post.Description,
			Content: e.HTML, Creator: e.Post.Author, Categories: e.Post.Tags,
		})
	}
	return marshalXML(feed)
}

// feedUpdated is when the newest entry changed. An empty feed is dated at the
// Unix epoch, so the same posts always produce the same bytes.
func feedUpdated(entries []feedEntry) time.Time {
	latest := time.Unix(0, 0).UTC()
	for _, e := range entries {
		if m := modified(e.Post); m.After(latest) {
			latest = m
		}
	}
	return latest
}

// Atom 1.0.

type atomFeed struct {
	XMLName  xml.Name    `xml:"feed"`
	NS       string      `xml:"xmlns,attr"`
	Lang     string      `xml:"xml:lang,attr"`
	Title    string      `xml:"title"`
	Subtitle string      `xml:"subtitle"`
	ID       string      `xml:"id"`
	Updated  string      `xml:"updated"`
	Link     atomLink    `xml:"link"`
	Author   atomAuthor  `xml:"author"`
	Gen      string      `xml:"generator"`
	Entries  []atomEntry `xml:"entry"`
}

type atomLink struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
}

type atomAuthor struct {
	Name string `xml:"name"`
}

type atomEntry struct {
	Title      string       `xml:"title"`
	ID         string       `xml:"id"`
	Link       atomLink     `xml:"link"`
	Published  string       `xml:"published"`
	Updated    string       `xml:"updated"`
	Author     *atomAuthor  `xml:"author"`
	Categories []atomTerm   `xml:"category"`
	Summary    string       `xml:"summary,omitempty"`
	Content    *atomContent `xml:"content"`
}

type atomTerm struct {
	Term string `xml:"term,attr"`
}

type atomContent struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}

func encodeAtom(st *site, entries []feedEntry) ([]byte, error) {
	feed := atomFeed{
		NS: "http://www.w3.org/2005/Atom", Lang: st.language, Title: st.title, Subtitle: st.description,
		ID: st.url + "/", Updated: feedUpdated(entries).Format(time.RFC3339), Link: atomLink{"alternate", st.url + "/"},
		Author: atomAuthor{st.title}, Gen: "BurzBlog",
	}
	for _, e := range entries {
		link := st.postURL(e.Post.Slug)
		entry := atomEntry{
			Title: e.Post.Title, ID: link, Link: atomLink{"alternate", link},
			Published: published(e.Post).Format(time.RFC3339), Updated: modified(e.Post).Format(time.RFC3339),
			Summary: e.Post.Description,
		}
		if e.Post.Author != "" {
			entry.Author = &atomAuthor{e.Post.Author}
		}
		for _, t := range e.Post.Tags {
			entry.Categories = append(entry.Categories, atomTerm{t})
		}
		if e.HTML != "" {
			entry.Content = &atomContent{"html", e.HTML}
		}
		feed.Entries = append(feed.Entries, entry)
	}
	return marshalXML(feed)
}

// JSON Feed 1.1.

type jsonFeed struct {
	Version     string     `json:"version"`
	Title       string     `json:"title"`
	HomePageURL string     `json:"home_page_url"`
	Description string     `json:"description"`
	Language    string     `json:"language"`
	Items       []jsonItem `json:"items"`
}

type jsonItem struct {
	ID            string       `json:"id"`
	URL           string       `json:"url"`
	Title         string       `json:"title"`
	ContentHTML   string       `json:"content_html,omitempty"`
	ContentText   string       `json:"content_text,omitempty"`
	Summary       string       `json:"summary,omitempty"`
	Image         string       `json:"image,omitempty"`
	DatePublished string       `json:"date_published"`
	DateModified  string       `json:"date_modified"`
	Authors       []jsonAuthor `json:"authors,omitempty"`
	Tags          []string     `json:"tags,omitempty"`
}

type jsonAuthor struct {
	Name string `json:"name"`
}

func encodeJSONFeed(st *site, entries []feedEntry) ([]byte, error) {
	feed := jsonFeed{
		Version: "https://jsonfeed.org/version/1.1", Title: st.title, HomePageURL: st.url + "/",
		Description: st.description, Language: st.language, Items: []jsonItem{},
	}
	for _, e := range entries {
		link := st.postURL(e.Post.Slug)
		item := jsonItem{
			ID: link, URL: link, Title: e.Post.Title, ContentHTML: e.HTML, Summary: e.Post.Description,
			Image: e.Post.CoverURL, DatePublished: published(e.Post).Format(time.RFC3339),
			DateModified: modified(e.Post).Format(time.RFC3339), Tags: e.Post.Tags,
		}
		if e.HTML == "" {
			// One of content_html and content_text is required.
			item.ContentText = cmp.Or(e.Post.Description, e.Post.Title)
		}
		if e.Post.Author != "" {
			item.Authors = []jsonAuthor{{e.Post.Author}}
		}
		feed.Items = append(feed.Items, item)
	}
	out, err := json.Marshal(feed)
	if err != nil {
		return nil, fmt.Errorf("encode json feed: %w", err)
	}
	return append(out, '\n'), nil
}

// Sitemap.

type urlSet struct {
	XMLName xml.Name     `xml:"urlset"`
	NS      string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

func encodeSitemap(st *site, list []store.Listing, tags []store.TagCount) ([]byte, error) {
	set := urlSet{NS: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	for _, l := range list {
		set.URLs = append(set.URLs, sitemapURL{st.postURL(l.Post.Slug), modified(l.Post).Format(time.RFC3339)})
	}
	for _, t := range tags {
		set.URLs = append(set.URLs, sitemapURL{Loc: st.tagURL(t.Tag)})
	}
	return marshalXML(set)
}
