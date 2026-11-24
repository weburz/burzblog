package server

import (
	"context"
	"io"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"github.com/weburz/burzblog/internal/render"
	"github.com/weburz/burzblog/internal/store"
)

// publishedHTML adjusts the already sanitized HTML for a reader's website.
// Preview links retain the panel's separate navigation policy.
func (s *Server) publishedHTML(ctx context.Context, slug, body string, sizes map[string]store.MediaSize) (string, error) {
	raw, err := render.HTML(ctx, body)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	z := html.NewTokenizer(strings.NewReader(raw))
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			if z.Err() != io.EOF {
				return "", z.Err()
			}
			return out.String(), nil
		}
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			out.Write(z.Raw())
			continue
		}
		t := z.Token()
		for i, a := range t.Attr {
			if a.Key == "id" && isFootnoteID(a.Val) {
				t.Attr[i].Val = slug + "-" + a.Val
			} else if a.Key == "href" && strings.HasPrefix(a.Val, "#") && isFootnoteID(a.Val[1:]) {
				t.Attr[i].Val = "#" + slug + "-" + a.Val[1:]
			}
		}
		switch t.Data {
		case "a":
			for _, attr := range t.Attr {
				if attr.Key == "href" && s.internalReaderLink(attr.Val) {
					t.Attr = slices.DeleteFunc(t.Attr, func(a html.Attribute) bool {
						return a.Key == "target"
					})
					for i, a := range t.Attr {
						if a.Key == "rel" {
							t.Attr[i].Val = strings.Join(slices.DeleteFunc(strings.Fields(a.Val), func(v string) bool { return v == "nofollow" }), " ")
						}
					}
					break
				}
			}
		case "img":
			for _, attr := range t.Attr {
				if attr.Key == "src" {
					if size, ok := sizes[s.coverMediaName(attr.Val)]; ok {
						t.Attr = append(t.Attr, html.Attribute{Key: "width", Val: strconv.Itoa(size.Width)}, html.Attribute{Key: "height", Val: strconv.Itoa(size.Height)})
					}
					break
				}
			}
		}
		out.WriteString(t.String())
	}
}

// Generated IDs use fn:N or fnrefK:N, where K is empty on the first reference.
func isFootnoteID(value string) bool {
	prefix, number, ok := strings.Cut(value, ":")
	if !ok {
		return false
	}
	if _, err := strconv.Atoi(number); err != nil {
		return false
	}
	if prefix == "fn" || prefix == "fnref" {
		return true
	}
	repeated, ok := strings.CutPrefix(prefix, "fnref")
	if !ok {
		return false
	}
	_, err := strconv.Atoi(repeated)
	return err == nil
}

func (s *Server) internalReaderLink(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || strings.Contains(raw, "\\") {
		return false
	}
	if u.Scheme == "" && u.Host == "" {
		return !strings.HasPrefix(raw, "//")
	}
	if s.site == nil {
		return false
	}
	if u.Scheme == "" {
		u.Scheme = s.site.origin.Scheme
	}
	return strings.EqualFold(u.Scheme, s.site.origin.Scheme) && strings.EqualFold(u.Host, s.site.origin.Host)
}
