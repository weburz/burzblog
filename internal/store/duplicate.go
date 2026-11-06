package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/weburz/burzblog/internal/post"
)

const copySlugSuffix = "-copy"

// copyMark matches the "(copy)" or "(copy 2)" that ends a duplicated title.
var copyMark = regexp.MustCompile(`\s*\(copy(?: (\d+))?\)$`)

// copyTitle returns the title of a copy of a post titled title. A title that
// already ends in "(copy)" or "(copy N)" counts up ("(copy 2)", "(copy 3)")
// instead of stacking marks. A title too long for the mark is cut at a word
// boundary, or mid-word when it has no space to cut at.
func copyTitle(title string) string {
	mark := "(copy)"
	if m := copyMark.FindStringSubmatch(title); m != nil {
		n := 1
		if m[1] != "" {
			if parsed, err := strconv.Atoi(m[1]); err == nil {
				n = parsed
			}
		}
		mark = fmt.Sprintf("(copy %d)", n+1)
		title = strings.TrimRight(title[:len(title)-len(m[0])], " ")
	}
	runes := []rune(title)
	if keep := post.MaxTitle - len([]rune(mark)) - 1; len(runes) > keep {
		cut := keep
		if runes[keep] != ' ' {
			for i := keep - 1; i > 0; i-- {
				if runes[i] == ' ' {
					cut = i
					break
				}
			}
		}
		runes = runes[:cut]
		title = strings.TrimRight(string(runes), " ")
	}
	if title == "" {
		return mark
	}
	return title + " " + mark
}

// DuplicatePost creates a draft from the post with the given slug. The copy
// keeps the content (title with " (copy)" added, or counted up to " (copy 2)", description, body, cover,
// tags, author, SEO fields and extra) and starts a fresh life: no publish or
// update dates, no schedule, not featured. Its slug is "<slug>-copy", then
// "<slug>-copy-2", "-3" and so on until one is free.
func (s *Store) DuplicatePost(ctx context.Context, slug, by string) (*post.Post, error) {
	src, err := s.GetPost(ctx, slug)
	if err != nil {
		return nil, err
	}
	title := copyTitle(src.Title)
	for n := 1; ; n++ {
		suffix := copySlugSuffix
		if n > 1 {
			suffix = fmt.Sprintf("%s-%d", copySlugSuffix, n)
		}
		base := src.Slug
		if keep := post.MaxSlug - len(suffix); len(base) > keep {
			base = strings.TrimRight(base[:keep], "-")
		}
		copied, err := s.CreatePost(ctx, &post.Post{
			Slug:           base + suffix,
			Title:          title,
			Description:    src.Description,
			Status:         post.Draft,
			Body:           src.Body,
			CoverURL:       src.CoverURL,
			CoverAlt:       src.CoverAlt,
			Extra:          src.Extra,
			Tags:           src.Tags,
			Author:         src.Author,
			SEOTitle:       src.SEOTitle,
			SEODescription: src.SEODescription,
			CanonicalURL:   src.CanonicalURL,
		}, by)
		if errors.Is(err, ErrTaken) {
			continue
		}
		return copied, err
	}
}
