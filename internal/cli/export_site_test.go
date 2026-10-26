package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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
