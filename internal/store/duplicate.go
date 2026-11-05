package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/weburz/burzblog/internal/post"
)

const (
	copySlugSuffix  = "-copy"
	copyTitleSuffix = " (copy)"
)

// DuplicatePost creates a draft from the post with the given slug. The copy
// keeps the content (title with " (copy)" added, description, body, cover,
// tags, author, SEO fields and extra) and starts a fresh life: no publish or
// update dates, no schedule, not featured. Its slug is "<slug>-copy", then
// "<slug>-copy-2", "-3" and so on until one is free.
func (s *Store) DuplicatePost(ctx context.Context, slug, by string) (*post.Post, error) {
	src, err := s.GetPost(ctx, slug)
	if err != nil {
		return nil, err
	}
	title := []rune(src.Title)
	if keep := post.MaxTitle - len([]rune(copyTitleSuffix)); len(title) > keep {
		title = title[:keep]
	}
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
			Title:          string(title) + copyTitleSuffix,
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
