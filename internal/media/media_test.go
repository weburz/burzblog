package media

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 7, 5))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestInspect(t *testing.T) {
	k, w, h, err := Inspect(pngBytes(t))
	if err != nil || k.MIME != "image/png" || w != 7 || h != 5 {
		t.Errorf("png = %+v %d %d %v", k, w, h, err)
	}
	for name, data := range map[string][]byte{
		"html":       []byte("<!doctype html><html></html>"),
		"svg":        []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`),
		"text":       []byte("hello"),
		"empty":      nil,
		"exe":        []byte("MZ\x90\x00"),
		"short ftyp": []byte("\x00\x00\x00\x1cftypmp42"),
	} {
		if _, _, _, err := Inspect(data); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: err = %v, want ErrUnsupported", name, err)
		}
	}
	if _, _, _, err := Inspect([]byte("\x89PNG\r\n\x1a\ntruncated")); !errors.Is(err, ErrCorrupt) {
		t.Errorf("truncated png: err = %v, want ErrCorrupt", err)
	}
}

func TestValidName(t *testing.T) {
	good := strings.Repeat("0f", 16)
	for _, ext := range []string{".jpg", ".png", ".gif", ".webp", ".avif", ".pdf"} {
		if !ValidName(good + ext) {
			t.Errorf("%s rejected", ext)
		}
	}
	for _, name := range []string{"", good, good + ".svg", good + ".png/", "../" + good + ".png", good + "/../x.png",
		strings.ToUpper(good) + ".png", good[:31] + ".png", ".." + good + ".png", good + ".png\x00"} {
		if ValidName(name) {
			t.Errorf("%q accepted", name)
		}
	}
}

func TestDisk(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "media")
	d, err := NewDisk(dir)
	if err != nil {
		t.Fatal(err)
	}
	name := strings.Repeat("ab", 16) + ".png"
	if _, _, err := d.Open(name); !errors.Is(err, ErrNotFound) {
		t.Errorf("open missing = %v", err)
	}
	if err := d.Put(name, []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := d.Put(name, []byte("two")); err != nil { // content-addressed: kept as is
		t.Fatal(err)
	}
	f, _, err := d.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	buf.ReadFrom(f)
	f.Close()
	if buf.String() != "one" {
		t.Errorf("content = %q", buf.String())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("leftover files: %v", entries)
	}
	if err := d.Put("../x.png", []byte("x")); err == nil {
		t.Error("put accepted a traversal name")
	}
	if _, _, err := d.Open("../x.png"); !errors.Is(err, ErrNotFound) {
		t.Errorf("open traversal = %v", err)
	}
	if err := d.Delete(name); err != nil {
		t.Fatal(err)
	}
	if err := d.Delete(name); err != nil {
		t.Errorf("delete twice = %v", err)
	}
}
