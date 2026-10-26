// Package media decides which uploads BurzBlog accepts and keeps their bytes.
// It knows nothing about HTTP or the database.
package media

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"regexp"
	"time"
)

// Errors callers can branch on.
var (
	// ErrUnsupported means the bytes are not one of the accepted formats.
	ErrUnsupported = errors.New("unsupported file type")
	// ErrCorrupt means the bytes start like an accepted image but do not decode.
	ErrCorrupt = errors.New("damaged image")
	// ErrNotFound means the storage has no file with that name.
	ErrNotFound = errors.New("not found")
)

// Kind describes an accepted format.
type Kind struct {
	// MIME is the type served for the file.
	MIME string
	// Ext is the file name extension, with the dot.
	Ext string
	// Inline is true when browsers may show the file in a page; other kinds
	// are served as downloads.
	Inline bool
}

var (
	jpegKind = Kind{"image/jpeg", ".jpg", true}
	pngKind  = Kind{"image/png", ".png", true}
	gifKind  = Kind{"image/gif", ".gif", true}
	webpKind = Kind{"image/webp", ".webp", true}
	avifKind = Kind{"image/avif", ".avif", true}
	pdfKind  = Kind{"application/pdf", ".pdf", false}
)

// kinds is every accepted format. SVG is deliberately absent: it is a script
// carrier, so it cannot be served from the panel's origin safely.
var kinds = []Kind{jpegKind, pngKind, gifKind, webpKind, avifKind, pdfKind}

// KindOf returns the format for a stored file's extension.
func KindOf(ext string) (Kind, bool) {
	for _, k := range kinds {
		if k.Ext == ext {
			return k, true
		}
	}
	return Kind{}, false
}

// Sniff decides the format of data from its leading bytes alone. The client's
// Content-Type and the file's name are never consulted.
func Sniff(data []byte) (Kind, error) {
	switch http.DetectContentType(data) {
	case jpegKind.MIME:
		return jpegKind, nil
	case pngKind.MIME:
		return pngKind, nil
	case gifKind.MIME:
		return gifKind, nil
	case webpKind.MIME:
		return webpKind, nil
	case pdfKind.MIME:
		return pdfKind, nil
	}
	if isAVIF(data) {
		return avifKind, nil
	}
	return Kind{}, ErrUnsupported
}

// isAVIF reports whether data begins with an ISO base media "ftyp" box whose
// major brand is avif or avis.
func isAVIF(data []byte) bool {
	return len(data) >= 12 && bytes.Equal(data[4:8], []byte("ftyp")) &&
		(bytes.Equal(data[8:12], []byte("avif")) || bytes.Equal(data[8:12], []byte("avis")))
}

// Inspect sniffs data and, for formats the standard library decodes (JPEG,
// PNG, GIF), reads the dimensions. Those must decode, which rejects files that
// only borrow an image's first bytes. WebP, AVIF and PDF report 0x0.
func Inspect(data []byte) (k Kind, width, height int, err error) {
	k, err = Sniff(data)
	if err != nil {
		return Kind{}, 0, 0, err
	}
	var cfg image.Config
	switch k {
	case jpegKind:
		cfg, err = jpeg.DecodeConfig(bytes.NewReader(data))
	case pngKind:
		cfg, err = png.DecodeConfig(bytes.NewReader(data))
	case gifKind:
		cfg, err = gif.DecodeConfig(bytes.NewReader(data))
	default:
		return k, 0, 0, nil
	}
	if err != nil {
		return Kind{}, 0, 0, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	return k, cfg.Width, cfg.Height, nil
}

// nameRE matches the names Storage hands out: 32 hex digits of the content
// hash and an accepted extension. Anything else is never read from disk.
var nameRE = regexp.MustCompile(`^[0-9a-f]{32}\.(jpg|png|gif|webp|avif|pdf)$`)

// ValidName reports whether name is a file name this package could have
// produced, which also rules out any path separator or dot segment.
func ValidName(name string) bool {
	return nameRE.MatchString(name)
}

// Name returns the storage name for a file with the given hex SHA-256 digest.
func Name(sha256Hex string, k Kind) string {
	return sha256Hex[:32] + k.Ext
}

// File is an opened stored file.
type File interface {
	io.ReadSeekCloser
}

// Storage keeps uploaded bytes by name. Disk is the only implementation; the
// interface lets an S3-compatible backend be added without touching handlers.
type Storage interface {
	// Put stores data under name. It is a no-op when name already exists, and
	// a reader never sees a partial file.
	Put(name string, data []byte) error
	// Open returns the file and its modification time, or ErrNotFound.
	Open(name string) (File, time.Time, error)
	// Delete removes the file; a missing file is not an error.
	Delete(name string) error
}
