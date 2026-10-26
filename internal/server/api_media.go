package server

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/weburz/burzblog/internal/media"
	"github.com/weburz/burzblog/internal/store"
)

// DefaultMaxUpload is the largest accepted upload unless configured otherwise.
const DefaultMaxUpload = 10 << 20

const (
	// maxUploadAlt is the longest alt text, in bytes.
	maxUploadAlt = 1000
	// maxOriginalName is the longest remembered file name, in runes.
	maxOriginalName = 200
	// uploadOverhead is how much multipart framing a request may carry beyond
	// the file itself.
	uploadOverhead = 1 << 20
	// inUseListed is how many posts a refused delete names.
	inUseListed = 10
	// mediaPrefix is where uploads are served.
	mediaPrefix = "/media/"
)

// mediaReply is one upload as the API shows it.
type mediaReply struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	URL          string    `json:"url"`
	OriginalName string    `json:"originalName"`
	MIME         string    `json:"mime"`
	Size         int64     `json:"size"`
	Width        int       `json:"width,omitempty"`
	Height       int       `json:"height,omitempty"`
	Alt          string    `json:"alt"`
	UploadedBy   string    `json:"uploadedBy"`
	CreatedAt    time.Time `json:"createdAt"`
}

// mediaURL is the stable public address of an upload. It is absolute, built
// from the public origin, so posts that embed it keep working in an export.
func (s *Server) mediaURL(name string) string {
	return s.origin.String() + mediaPrefix + name
}

func (s *Server) mediaJSON(m *store.Media) mediaReply {
	return mediaReply{
		ID: m.ID, Name: m.FileName, URL: s.mediaURL(m.FileName), OriginalName: m.OriginalName, MIME: m.MIME,
		Size: m.Size, Width: m.Width, Height: m.Height, Alt: m.Alt, UploadedBy: m.UploadedBy, CreatedAt: m.CreatedAt,
	}
}

// maxUpload is the configured upload size limit.
func (s *Server) maxUpload() int64 {
	if s.cfg.MaxUpload > 0 {
		return s.cfg.MaxUpload
	}
	return DefaultMaxUpload
}

// cleanOriginalName reduces a client-supplied file name to a harmless label:
// the last path element, without control characters. It is only ever shown,
// never used to build a path.
func cleanOriginalName(name string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	if name == "." || name == "/" {
		return ""
	}
	if utf8.RuneCountInString(name) > maxOriginalName {
		name = string([]rune(name)[:maxOriginalName])
	}
	return name
}

// readUpload reads the multipart form: one "file" part and an optional "alt"
// text. It answers the error itself and reports false on failure.
func (s *Server) readUpload(w http.ResponseWriter, r *http.Request) (data []byte, filename, alt string, ok bool) {
	limit := s.maxUpload()
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "multipart/form-data" {
		fail(w, http.StatusUnsupportedMediaType, "content_type", "Send the file as multipart/form-data with a \"file\" field.", "")
		return nil, "", "", false
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit+uploadOverhead)
	mr, err := r.MultipartReader()
	if err != nil {
		fail(w, http.StatusBadRequest, "bad_request", "The upload was not valid multipart/form-data.", err.Error())
		return nil, "", "", false
	}
	tooBig := func() {
		fail(w, http.StatusRequestEntityTooLarge, "too_large",
			"That file is larger than the upload limit of "+strconv.FormatInt(limit>>10, 10)+" KiB.", "")
	}
	found := false
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			var big *http.MaxBytesError
			if errors.As(err, &big) {
				tooBig()
			} else {
				fail(w, http.StatusBadRequest, "bad_request", "The upload was not valid multipart/form-data.", err.Error())
			}
			return nil, "", "", false
		}
		switch part.FormName() {
		case "file":
			if found {
				fail(w, http.StatusBadRequest, "bad_request", "Send one file per upload.", "")
				return nil, "", "", false
			}
			data, err = io.ReadAll(io.LimitReader(part, limit+1))
			if err != nil {
				var big *http.MaxBytesError
				if errors.As(err, &big) {
					tooBig()
				} else {
					fail(w, http.StatusBadRequest, "bad_request", "The upload could not be read.", err.Error())
				}
				return nil, "", "", false
			}
			if int64(len(data)) > limit {
				tooBig()
				return nil, "", "", false
			}
			filename, found = part.FileName(), true
		case "alt":
			b, err := io.ReadAll(io.LimitReader(part, maxUploadAlt+1))
			if err != nil || len(b) > maxUploadAlt || !utf8.Valid(b) {
				fail(w, http.StatusUnprocessableEntity, "validation", "Alt text must be text of at most 1000 bytes.", "")
				return nil, "", "", false
			}
			alt = strings.TrimSpace(string(b))
		}
	}
	if !found || len(data) == 0 {
		fail(w, http.StatusUnprocessableEntity, "validation", "Attach a non-empty file in the \"file\" field.", "")
		return nil, "", "", false
	}
	return data, filename, alt, true
}

// uploadMedia accepts one image or PDF. What it is comes from its bytes, not
// from the name or Content-Type the client claims. The same bytes uploaded
// twice are stored once and answer 200 with the existing record.
func (s *Server) uploadMedia(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Media == nil {
		fail(w, http.StatusNotImplemented, "media_disabled", "Uploads are not set up on this server.", "")
		return
	}
	data, filename, alt, ok := s.readUpload(w, r)
	if !ok {
		return
	}
	kind, width, height, err := media.Inspect(data)
	switch {
	case errors.Is(err, media.ErrUnsupported):
		fail(w, http.StatusUnsupportedMediaType, "unsupported_type",
			"Upload a JPEG, PNG, GIF, WebP, AVIF or PDF file. SVG is not accepted.", "")
		return
	case errors.Is(err, media.ErrCorrupt):
		fail(w, http.StatusUnprocessableEntity, "validation", "That image is damaged or is not what its first bytes claim.", "")
		return
	case err != nil:
		s.internal(w, r, err)
		return
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	name := media.Name(digest, kind)
	s.mediaMu.Lock()
	defer s.mediaMu.Unlock()
	if err := s.cfg.Media.Put(name, data); err != nil {
		s.internal(w, r, err)
		return
	}
	m, created, err := s.store.AddMedia(r.Context(), store.Media{
		FileName: name, OriginalName: cleanOriginalName(filename), MIME: kind.MIME, Size: int64(len(data)),
		SHA256: digest, Width: width, Height: height, Alt: alt, UploadedBy: who(r).user.Username,
	})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
		s.record(r, "media.upload", m.FileName)
	}
	reply(w, status, s.mediaJSON(m))
}

// listMedia lists uploads, newest first. Without ?limit= it returns all.
func (s *Server) listMedia(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := pageParams(w, r)
	if !ok {
		return
	}
	list, total, err := s.store.ListMedia(r.Context(), limit, offset)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	items := make([]mediaReply, 0, len(list))
	for i := range list {
		items = append(items, s.mediaJSON(&list[i]))
	}
	reply(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

// deleteMedia removes an upload and its bytes. Posts that mention it would
// show a broken image, so it answers 409 naming them unless ?force=true.
func (s *Server) deleteMedia(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, http.StatusNotFound, "not_found", "That upload does not exist. It may already be deleted.", "")
		return
	}
	s.mediaMu.Lock()
	defer s.mediaMu.Unlock()
	m, err := s.store.GetMedia(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "not_found", "That upload does not exist. It may already be deleted.", "")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if r.URL.Query().Get("force") != "true" {
		users, err := s.store.MediaUsers(r.Context(), m.FileName, inUseListed)
		if err != nil {
			s.internal(w, r, err)
			return
		}
		if len(users) > 0 {
			fail(w, http.StatusConflict, "in_use",
				"Posts still use this file: "+strings.Join(users, ", ")+". Remove it from them, or delete with force=true to break those links.", "")
			return
		}
	}
	if _, err := s.store.DeleteMedia(r.Context(), id); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.internal(w, r, err)
		return
	}
	if err := s.cfg.Media.Delete(m.FileName); err != nil {
		s.internal(w, r, err)
		return
	}
	s.record(r, "media.delete", m.FileName)
	reply(w, http.StatusOK, map[string]bool{"deleted": true})
}

// serveMedia serves an upload at its public address. This is the one route
// outside /api that answers without a sign-in: uploads are immutable assets
// the author chose to put in posts, found only by their unguessable
// content-hash names. The name is matched against a strict pattern before the
// storage is touched, and the response cannot run script: nosniff, a
// sandboxed no-resource CSP, and PDFs as downloads.
func (s *Server) serveMedia(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	kind, ok := media.KindOf(path.Ext(name))
	if s.cfg.Media == nil || !ok || !media.ValidName(name) {
		http.NotFound(w, r)
		return
	}
	f, modTime, err := s.cfg.Media.Open(name)
	if errors.Is(err, media.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("Content-Type", kind.MIME)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	// Websites on other origins embed these images.
	h.Set("Cross-Origin-Resource-Policy", "cross-origin")
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("ETag", `"`+name+`"`)
	if kind.Inline {
		h.Set("Content-Disposition", "inline")
	} else {
		h.Set("Content-Disposition", `attachment; filename="`+name+`"`)
	}
	http.ServeContent(w, r, name, modTime, f)
}

// addMediaToTar adds every upload to a backup as media/<name>. A file missing
// from the storage is logged and skipped, so one lost file does not spoil the
// whole archive.
func (s *Server) addMediaToTar(r *http.Request, tw *tar.Writer) error {
	if s.cfg.Media == nil {
		return nil
	}
	list, _, err := s.store.ListMedia(r.Context(), 0, 0)
	if err != nil {
		return err
	}
	for _, m := range list {
		f, modTime, err := s.cfg.Media.Open(m.FileName)
		if errors.Is(err, media.ErrNotFound) {
			s.log.Warn("backup: upload is missing from storage", "name", m.FileName)
			continue
		}
		if err != nil {
			return err
		}
		err = addFileToTar(tw, "media/"+m.FileName, f, modTime)
		f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// addFileToTar adds the whole of f, whose size is found by seeking.
func addFileToTar(tw *tar.Writer, name string, f media.File, modTime time.Time) error {
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	hdr := &tar.Header{Name: name, Mode: 0o644, Size: size, ModTime: modTime, Typeflag: tar.TypeReg, Format: tar.FormatPAX}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err = io.CopyN(tw, f, size)
	return err
}
