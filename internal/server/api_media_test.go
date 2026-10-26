package server

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weburz/burzblog/internal/media"
	"github.com/weburz/burzblog/internal/store"
)

func newMediaHarness(t *testing.T, mutate func(*Config)) (*harness, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "media")
	disk, err := media.NewDisk(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, func(c *Config) {
		c.Media = disk
		if mutate != nil {
			mutate(c)
		}
	})
	return h, dir
}

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testJPEG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 3, 2)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testGIF(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := gif.Encode(&buf, image.NewPaletted(image.Rect(0, 0, 4, 4), color.Palette{color.Black, color.White}), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

var (
	testWebP = append([]byte("RIFF\x1a\x00\x00\x00WEBPVP8 "), make([]byte, 20)...)
	testAVIF = append([]byte("\x00\x00\x00\x1cftypavif\x00\x00\x00\x00avifmif1"), make([]byte, 16)...)
	testPDF  = []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\ntrailer\n<<>>\n%%EOF\n")
)

// multipartBody builds an upload form. The file part claims claimedType.
func multipartBody(t *testing.T, filename, claimedType string, data []byte, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
	hdr.Set("Content-Type", claimedType)
	part, err := mw.CreatePart(hdr)
	if err != nil {
		t.Fatal(err)
	}
	part.Write(data)
	mw.Close()
	return &buf, mw.FormDataContentType()
}

// upload posts a file as the browser panel would.
func (h *harness) upload(filename, claimedType string, data []byte, fields map[string]string) (*http.Response, map[string]any) {
	h.t.Helper()
	body, ct := multipartBody(h.t, filename, claimedType, data, fields)
	req, _ := http.NewRequest("POST", h.ts.URL+"/api/media", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Origin", h.ts.URL)
	if h.csrf != "" {
		req.Header.Set("X-CSRF-Token", h.csrf)
	}
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return resp, out
}

func mediaFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestUploadAcceptsEveryAllowedType(t *testing.T) {
	h, dir := newMediaHarness(t, nil)
	h.login()
	cases := []struct {
		name          string
		data          []byte
		mime, ext     string
		width, height float64
	}{
		{"a.png", testPNG(t, 5, 3), "image/png", ".png", 5, 3},
		{"b.jpeg", testJPEG(t), "image/jpeg", ".jpg", 3, 2},
		{"c.gif", testGIF(t), "image/gif", ".gif", 4, 4},
		{"d.webp", testWebP, "image/webp", ".webp", 0, 0},
		{"e.avif", testAVIF, "image/avif", ".avif", 0, 0},
		{"f.pdf", testPDF, "application/pdf", ".pdf", 0, 0},
	}
	for _, c := range cases {
		// The client claims a type that is wrong on purpose: it is ignored.
		resp, out := h.upload(c.name, "application/x-whatever", c.data, map[string]string{"alt": "A picture"})
		if resp.StatusCode != 201 {
			t.Fatalf("%s: upload = %d %v", c.name, resp.StatusCode, out)
		}
		name, _ := out["name"].(string)
		if out["mime"] != c.mime || !strings.HasSuffix(name, c.ext) || len(name) != 32+len(c.ext) {
			t.Errorf("%s: reply = %v", c.name, out)
		}
		if w, _ := out["width"].(float64); w != c.width {
			t.Errorf("%s: width = %v, want %v", c.name, out["width"], c.width)
		}
		if out["url"] != h.ts.URL+"/media/"+name || out["alt"] != "A picture" || out["uploaded_by"] != "ada" || out["original_name"] != c.name {
			t.Errorf("%s: reply = %v", c.name, out)
		}
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s: not stored: %v", c.name, err)
		}
	}
}

func TestUploadRejectsWhatItShouldNot(t *testing.T) {
	h, dir := newMediaHarness(t, func(c *Config) { c.MaxUpload = 2048 })
	h.login()
	cases := []struct {
		name, claimed string
		data          []byte
		status        int
		code          string
	}{
		{"html renamed png", "image/png", []byte("<html><script>alert(1)</script></html>"), 415, "unsupported_type"},
		{"svg", "image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), 415, "unsupported_type"},
		{"svg renamed png", "image/png", []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"/>`), 415, "unsupported_type"},
		{"script renamed jpg", "image/jpeg", []byte("#!/bin/sh\necho hi\n"), 415, "unsupported_type"},
		{"png header only", "image/png", []byte("\x89PNG\r\n\x1a\nnot really a png"), 422, "validation"},
		{"empty", "image/png", nil, 422, "validation"},
		{"oversize", "image/png", append(testPNG(t, 2, 2), make([]byte, 4096)...), 413, "too_large"},
	}
	for _, c := range cases {
		resp, out := h.upload("x.png", c.claimed, c.data, nil)
		if resp.StatusCode != c.status || errCode(out) != c.code {
			t.Errorf("%s: = %d %v, want %d %s", c.name, resp.StatusCode, out, c.status, c.code)
		}
	}
	if got := mediaFiles(t, dir); len(got) != 0 {
		t.Errorf("rejected uploads left files: %v", got)
	}
	if _, total, _ := h.store.ListMedia(t.Context(), 0, 0); total != 0 {
		t.Errorf("rejected uploads left %d rows", total)
	}
}

func TestUploadNeedsMultipartAndFileField(t *testing.T) {
	h, _ := newMediaHarness(t, nil)
	h.login()
	if resp, out := h.do("POST", "/api/media", map[string]string{"file": "x"}); resp.StatusCode != 415 || errCode(out) != "content_type" {
		t.Errorf("json upload = %d %v", resp.StatusCode, out)
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("other", "value")
	mw.Close()
	req, _ := http.NewRequest("POST", h.ts.URL+"/api/media", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Origin", h.ts.URL)
	req.Header.Set("X-CSRF-Token", h.csrf)
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 422 {
		t.Errorf("missing file field = %d, want 422", resp.StatusCode)
	}
}

func TestUploadNameIsNeverTheClientsName(t *testing.T) {
	h, dir := newMediaHarness(t, nil)
	h.login()
	for _, name := range []string{"../../etc/passwd.png", `..\..\evil.png`, "/abs/path.png", "a/b/../../c.png"} {
		resp, out := h.upload(name, "image/png", testPNG(t, 1+len(name), 1), nil)
		if resp.StatusCode != 201 {
			t.Fatalf("%q: upload = %d %v", name, resp.StatusCode, out)
		}
		stored, _ := out["name"].(string)
		if !media.ValidName(stored) {
			t.Errorf("%q: stored as %q", name, stored)
		}
		if orig, _ := out["original_name"].(string); strings.ContainsAny(orig, `/\`) || strings.Contains(orig, "\x00") {
			t.Errorf("%q: original_name = %q", name, orig)
		}
	}
	for _, name := range mediaFiles(t, dir) {
		if !media.ValidName(name) {
			t.Errorf("unexpected file %q in the media folder", name)
		}
	}
	if entries, _ := os.ReadDir(filepath.Dir(dir)); len(entries) != 1 {
		t.Errorf("files escaped the media folder: %v", entries)
	}
}

func TestUploadDedupes(t *testing.T) {
	h, dir := newMediaHarness(t, nil)
	h.login()
	data := testPNG(t, 4, 4)
	resp, first := h.upload("one.png", "image/png", data, map[string]string{"alt": "first"})
	if resp.StatusCode != 201 {
		t.Fatalf("first = %d %v", resp.StatusCode, first)
	}
	resp, second := h.upload("two.png", "image/png", data, map[string]string{"alt": "second"})
	if resp.StatusCode != 200 || second["id"] != first["id"] || second["name"] != first["name"] || second["alt"] != "first" {
		t.Errorf("second = %d %v, want the first record unchanged", resp.StatusCode, second)
	}
	if got := mediaFiles(t, dir); len(got) != 1 {
		t.Errorf("files = %v, want one", got)
	}
	if _, out := h.do("GET", "/api/media", nil); out["total"] != float64(1) {
		t.Errorf("list = %v", out)
	}
}

func TestUploadGates(t *testing.T) {
	h, dir := newMediaHarness(t, nil)
	data := testPNG(t, 2, 2)

	// Anonymous.
	if resp, out := h.upload("a.png", "image/png", data, nil); resp.StatusCode != 401 {
		t.Errorf("anonymous upload = %d %v", resp.StatusCode, out)
	}
	h.login()

	// Missing CSRF token.
	csrf := h.csrf
	h.csrf = ""
	if resp, out := h.upload("a.png", "image/png", data, nil); resp.StatusCode != 403 || errCode(out) != "csrf" {
		t.Errorf("no CSRF = %d %v", resp.StatusCode, out)
	}
	h.csrf = csrf

	// Foreign Origin.
	body, ct := multipartBody(t, "a.png", "image/png", data, nil)
	req, _ := http.NewRequest("POST", h.ts.URL+"/api/media", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("X-CSRF-Token", h.csrf)
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Errorf("foreign origin = %d, want 403", resp.StatusCode)
	}

	// An export token cannot upload, list or delete.
	writeSecret := h.issueToken(store.ScopeWrite)
	h2 := h.secondBrowser()
	h2.token = h.issueToken(store.ScopeExport)
	if resp, out := h2.upload("a.png", "image/png", data, nil); resp.StatusCode != 403 || errCode(out) != "scope" {
		t.Errorf("export token upload = %d %v", resp.StatusCode, out)
	}
	if got := mediaFiles(t, dir); len(got) != 0 {
		t.Errorf("refused uploads left files: %v", got)
	}

	// A write token can, without CSRF.
	h3 := h.secondBrowser()
	h3.token = writeSecret
	if resp, out := h3.upload("a.png", "image/png", data, nil); resp.StatusCode != 201 {
		t.Errorf("write token upload = %d %v", resp.StatusCode, out)
	}
}

func TestUploadsDisabledWithoutStorage(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	if resp, out := h.upload("a.png", "image/png", testPNG(t, 1, 1), nil); resp.StatusCode != 501 || errCode(out) != "media_disabled" {
		t.Errorf("upload = %d %v", resp.StatusCode, out)
	}
}

func TestServeMediaHeaders(t *testing.T) {
	h, _ := newMediaHarness(t, nil)
	h.login()
	_, img := h.upload("a.png", "image/png", testPNG(t, 2, 2), nil)
	_, doc := h.upload("a.pdf", "application/pdf", testPDF, nil)

	get := func(url string, hdr map[string]string) *http.Response {
		req, _ := http.NewRequest("GET", url, nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req) // no cookie: it is public
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	resp := get(img["url"].(string), nil)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !bytes.HasPrefix(body, []byte("\x89PNG")) {
		t.Fatalf("image = %d (%d bytes)", resp.StatusCode, len(body))
	}
	want := map[string]string{
		"Content-Type":                 "image/png",
		"X-Content-Type-Options":       "nosniff",
		"Content-Disposition":          "inline",
		"Content-Security-Policy":      "default-src 'none'; sandbox",
		"Cache-Control":                "public, max-age=31536000, immutable",
		"Cross-Origin-Resource-Policy": "cross-origin",
		"ETag":                         `"` + img["name"].(string) + `"`,
	}
	for k, v := range want {
		if got := resp.Header.Get(k); got != v {
			t.Errorf("image %s = %q, want %q", k, got, v)
		}
	}

	again := get(img["url"].(string), map[string]string{"If-None-Match": want["ETag"]})
	again.Body.Close()
	if again.StatusCode != 304 {
		t.Errorf("conditional get = %d, want 304", again.StatusCode)
	}

	pdf := get(doc["url"].(string), nil)
	pdf.Body.Close()
	if cd := pdf.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") || pdf.Header.Get("Content-Type") != "application/pdf" {
		t.Errorf("pdf: Content-Disposition = %q, Content-Type = %q", cd, pdf.Header.Get("Content-Type"))
	}
}

func TestServeMediaRefusesOtherNames(t *testing.T) {
	h, dir := newMediaHarness(t, nil)
	// A file in the folder that Storage did not name must not be served, and
	// neither must anything outside it.
	os.WriteFile(filepath.Join(dir, "notes.html"), []byte("<script>1</script>"), 0o600)
	os.WriteFile(filepath.Join(filepath.Dir(dir), "secret.png"), []byte("secret"), 0o600)
	for _, path := range []string{
		"/media/notes.html", "/media/..%2Fsecret.png", "/media/%2e%2e%2fburzblog.db", "/media/../secret.png",
		"/media/" + strings.Repeat("a", 32) + ".png", // valid shape, absent
		"/media/" + strings.Repeat("A", 32) + ".png", "/media/" + strings.Repeat("a", 32) + ".svg",
		"/media/", "/media/" + strings.Repeat("a", 32) + ".png/x",
	} {
		req, _ := http.NewRequest("GET", h.ts.URL+path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == 200 && !bytes.Contains(body, []byte("<html")) { // the panel's index is not an upload
			t.Errorf("%s = 200 %q", path, body)
		}
		if bytes.Contains(body, []byte("secret")) || bytes.Contains(body, []byte("<script>1")) {
			t.Errorf("%s leaked a file: %q", path, body)
		}
	}
}

func TestListAndDeleteMedia(t *testing.T) {
	h, dir := newMediaHarness(t, nil)
	h.login()
	var ids []float64
	for i := 1; i <= 3; i++ {
		_, out := h.upload("p.png", "image/png", testPNG(t, i, i), nil)
		ids = append(ids, out["id"].(float64))
	}
	_, out := h.do("GET", "/api/media?limit=2", nil)
	items := out["items"].([]any)
	if len(items) != 2 || out["total"] != float64(3) || items[0].(map[string]any)["id"] != ids[2] {
		t.Errorf("page 1 = %v", out)
	}
	_, out = h.do("GET", "/api/media?limit=2&offset=2", nil)
	if items := out["items"].([]any); len(items) != 1 {
		t.Errorf("page 2 = %v", out)
	}
	if resp, out := h.do("GET", "/api/media?limit=0", nil); resp.StatusCode != 422 {
		t.Errorf("bad limit = %d %v", resp.StatusCode, out)
	}

	// Used by a post: refused, then forced.
	_, first := h.do("GET", "/api/media?limit=3", nil)
	target := first["items"].([]any)[2].(map[string]any)
	h.do("POST", "/api/posts", map[string]any{"slug": "uses-it", "title": "Uses it", "body": "![x](" + target["url"].(string) + ")"})
	idPath := "/api/media/" + jsonNumber(target["id"])
	if resp, out := h.do("DELETE", idPath, nil); resp.StatusCode != 409 || errCode(out) != "in_use" || !strings.Contains(out["_raw"].(string), "uses-it") {
		t.Errorf("delete in use = %d %v", resp.StatusCode, out)
	}
	if got := mediaFiles(t, dir); len(got) != 3 {
		t.Errorf("refused delete changed files: %v", got)
	}
	if resp, out := h.do("DELETE", idPath+"?force=true", nil); resp.StatusCode != 200 {
		t.Errorf("forced delete = %d %v", resp.StatusCode, out)
	}
	if got := mediaFiles(t, dir); len(got) != 2 {
		t.Errorf("files after delete = %v", got)
	}
	resp, err := http.Get(target["url"].(string))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("deleted file serves %d, want 404", resp.StatusCode)
	}

	// Unused: deleted without force. Unknown: 404.
	last := items[0].(map[string]any)
	if resp, out := h.do("DELETE", "/api/media/"+jsonNumber(last["id"]), nil); resp.StatusCode != 200 {
		t.Errorf("delete unused = %d %v", resp.StatusCode, out)
	}
	for _, id := range []string{"999999", "abc", "-1"} {
		if resp, out := h.do("DELETE", "/api/media/"+id, nil); resp.StatusCode != 404 {
			t.Errorf("delete %s = %d %v", id, resp.StatusCode, out)
		}
	}
}

func jsonNumber(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestBackupIncludesMedia(t *testing.T) {
	h, _ := newMediaHarness(t, nil)
	h.login()
	data := testPNG(t, 3, 3)
	_, up := h.upload("a.png", "image/png", data, nil)
	h.upload("doc.pdf", "application/pdf", testPDF, nil)

	req, _ := http.NewRequest("GET", h.ts.URL+"/api/backup.tar.gz", nil)
	req.Header.Set("Origin", h.ts.URL)
	resp, err := h.client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("backup = %v %v", err, resp)
	}
	defer resp.Body.Close()
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	got := map[string][]byte{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got[hdr.Name], _ = io.ReadAll(tr)
	}
	if len(got) != 3 || got["README.txt"] == nil {
		var names []string
		for n := range got {
			names = append(names, n)
		}
		t.Fatalf("backup entries = %v", names)
	}
	if !bytes.Equal(got["media/"+up["name"].(string)], data) {
		t.Error("backup media differs from the upload")
	}
}

func TestBackupSkipsMissingMediaFile(t *testing.T) {
	h, dir := newMediaHarness(t, nil)
	h.login()
	_, up := h.upload("a.png", "image/png", testPNG(t, 3, 3), nil)
	os.Remove(filepath.Join(dir, up["name"].(string)))
	req, _ := http.NewRequest("GET", h.ts.URL+"/api/backup.tar.gz", nil)
	resp, err := h.client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("backup = %v %v", err, resp)
	}
	defer resp.Body.Close()
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	n := 0
	for {
		if _, err := tr.Next(); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n != 1 {
		t.Errorf("entries = %d, want only README.txt", n)
	}
}
