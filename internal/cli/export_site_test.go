package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func archive(t *testing.T, files map[string]string) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(data))
	}
	tw.Close()
	gz.Close()
	return bytes.NewReader(buf.Bytes())
}

func TestReadExportSeparatesSiteFiles(t *testing.T) {
	posts, site, err := readExport(archive(t, map[string]string{
		"hello.md": "post", "feed.xml": "<rss/>", "feed.atom": "<feed/>", "feed.json": "{}", "sitemap.xml": "<urlset/>",
	}))
	if err != nil || len(posts) != 1 || string(posts["hello.md"]) != "post" || len(site) != 4 || string(site["feed.xml"]) != "<rss/>" {
		t.Fatalf("readExport = %v %v %v", posts, site, err)
	}
}

func TestReadExportRefusesOtherFiles(t *testing.T) {
	for _, name := range []string{"other.xml", "dir/feed.xml", "../feed.xml", "Feed.xml", "robots.txt"} {
		if _, _, err := readExport(archive(t, map[string]string{name: "x"})); err == nil || !strings.Contains(err.Error(), "unexpected entry") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestCleanExportKeepsLocalEdits(t *testing.T) {
	dir := t.TempDir()
	posts := map[string][]byte{"old.md": []byte("published text")}
	if _, err := writeExport(dir, posts, nil, false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "old.md")
	if err := os.WriteFile(path, []byte("local content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if removed, err := writeExport(dir, nil, nil, true); err != nil || removed != 0 {
		t.Fatalf("clean: removed %d, error %v", removed, err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "local content" {
		t.Fatalf("local content: %q, %v", data, err)
	}
}
