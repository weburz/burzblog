package sitedocs

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommittedPagesMatchTheGuides(t *testing.T) {
	out := t.TempDir()
	if err := Build("../../docs", out); err != nil {
		t.Fatalf("Build: %v", err)
	}
	want, _ := filepath.Glob(filepath.Join(out, "*.html"))
	have, _ := filepath.Glob("../../site/docs/*.html")
	if len(want) != len(have) {
		t.Fatalf("site/docs has %d pages, the guides make %d: run task site:docs", len(have), len(want))
	}
	for _, w := range want {
		name := filepath.Base(w)
		a, _ := os.ReadFile(w)
		b, err := os.ReadFile(filepath.Join("../../site/docs", name))
		if err != nil || !bytes.Equal(a, b) {
			t.Errorf("site/docs/%s is out of date: run task site:docs", name)
		}
	}
}

func TestRewrite(t *testing.T) {
	for dest, want := range map[string]string{
		"security.md":              "security.html",
		"api.md#tag-labels":        "api.html#tag-labels",
		"#media":                   "#media",
		"https://coolify.io":       "https://coolify.io",
		"mailto:a@example.com":     "mailto:a@example.com",
		"../deploy/nginx.conf":     Repo + "deploy/nginx.conf",
		"images/editor-light.png":  Repo + "docs/images/editor-light.png",
		"../README.md#quick-start": Repo + "README.md#quick-start",
	} {
		if got := rewrite(dest); got != want {
			t.Errorf("rewrite(%q) = %q, want %q", dest, got, want)
		}
	}
}

func writeDocs(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for _, g := range Groups {
		for _, n := range g.Names {
			body := "# " + n + "\n\nAbout " + n + ".\n"
			if f, ok := files[n]; ok {
				body = f
			}
			if err := os.WriteFile(filepath.Join(dir, n+".md"), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	for n, body := range files {
		if err := os.WriteFile(filepath.Join(dir, n+".md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestRenderPage(t *testing.T) {
	dir := writeDocs(t, map[string]string{
		"api":      "# HTTP API\n\nThe API.\n\n## Posts\n\nSee [security](security.md#keys).\n",
		"security": "# Security\n\nIntro.\n\n## Keys\n\nText.\n",
	})
	pages, err := Render(dir)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var api Page
	for _, p := range pages {
		if p.Name == "api" {
			api = p
		}
	}
	if api.Title != "HTTP API" || api.Summary != "The API." {
		t.Errorf("title/summary = %q/%q", api.Title, api.Summary)
	}
	if len(api.Headings) != 1 || api.Headings[0].ID != "posts" {
		t.Errorf("headings = %+v", api.Headings)
	}
	body := string(api.Body)
	if strings.Contains(body, "<h1") {
		t.Error("the body repeats the title")
	}
	if !strings.Contains(body, `href="security.html#keys"`) {
		t.Errorf("link not rewritten: %s", body)
	}
}

func TestRenderRejectsBrokenAnchors(t *testing.T) {
	dir := writeDocs(t, map[string]string{"api": "# API\n\nSee [x](security.md#nope).\n"})
	if _, err := Render(dir); err == nil || !strings.Contains(err.Error(), "security.md#nope") {
		t.Fatalf("err = %v, want a broken-anchor error", err)
	}
}

func TestRenderRejectsUnlistedGuides(t *testing.T) {
	dir := writeDocs(t, map[string]string{"stray": "# Stray\n"})
	if _, err := Render(dir); err == nil || !strings.Contains(err.Error(), "stray.md is not on the site") {
		t.Fatalf("err = %v, want an unlisted-guide error", err)
	}
}

func TestRenderRejectsMissingTitle(t *testing.T) {
	dir := writeDocs(t, map[string]string{"api": "No title here.\n"})
	if _, err := Render(dir); err == nil || !strings.Contains(err.Error(), "no top-level # title") {
		t.Fatalf("err = %v, want a missing-title error", err)
	}
}
