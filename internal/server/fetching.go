package server

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"git.subcult.tv/subculture-collective/zelda/internal/fetch"
	"git.subcult.tv/subculture-collective/zelda/internal/htmlmeta"
	"git.subcult.tv/subculture-collective/zelda/internal/linkimport"
)

// Outbound fetching for link-page import (#7) and link previews (#8).
// All requests go through internal/fetch, which refuses private addresses.

const htmlAccept = "text/html,application/xhtml+xml;q=0.9,*/*;q=0.1"

// SetFetcher replaces the outbound HTTP client (tests use a loopback-friendly policy).
func (s *Server) SetFetcher(c *fetch.Client) { s.fetcher = c }

// fetchErr writes a 422 explaining why a remote fetch failed.
func (s *Server) fetchErr(w http.ResponseWriter, err error) {
	var code, msg string
	var ne interface{ Timeout() bool }
	switch {
	case errors.Is(err, fetch.ErrBlocked):
		code, msg = "blocked_address", "that address is on a private or local network, so it can't be fetched"
	case errors.Is(err, fetch.ErrURL):
		code, msg = "invalid", "enter a public http(s) link on the standard port"
	case errors.Is(err, fetch.ErrTooLarge):
		code, msg = "too_large", "that page or image is too large to read"
	case errors.Is(err, fetch.ErrRedirects):
		code, msg = "too_many_redirects", "that link redirects too many times"
	case errors.Is(err, fetch.ErrStatus):
		code, msg = "fetch_failed", "the site didn't return the page ("+strings.TrimPrefix(err.Error(), fetch.ErrStatus.Error()+": ")+")"
	case errors.As(err, &ne) && ne.Timeout(), errors.Is(err, context.DeadlineExceeded):
		code, msg = "fetch_failed", "the site took too long to respond"
	default:
		code, msg = "fetch_failed", "couldn't reach that site"
	}
	writeErr(w, http.StatusUnprocessableEntity, code, msg, "url")
}

// remoteURL reads {url} from the body and adds https:// when the scheme is missing.
func remoteURL(w http.ResponseWriter, r *http.Request) (string, bool) {
	var body struct {
		URL string `json:"url"`
	}
	if !decodeJSON(w, r, &body) {
		return "", false
	}
	u := strings.TrimSpace(body.URL)
	if u == "" || len(u) > 2000 {
		writeErr(w, http.StatusUnprocessableEntity, "invalid", "enter a link", "url")
		return "", false
	}
	if !strings.Contains(u, "://") {
		u = "https://" + u
	}
	return u, true
}

// fetchHTML fetches a web page, writing an error response on failure.
func (s *Server) fetchHTML(ctx context.Context, w http.ResponseWriter, rawURL string) *fetch.Response {
	res, err := s.fetcher.Get(ctx, rawURL, htmlAccept, fetch.MaxHTML)
	if err != nil {
		s.fetchErr(w, err)
		return nil
	}
	mt := res.MediaType
	if mt == "" {
		mt = strings.SplitN(http.DetectContentType(res.Body), ";", 2)[0]
	}
	if mt != "text/html" && mt != "application/xhtml+xml" {
		writeErr(w, http.StatusUnprocessableEntity, "not_html", "that link isn't a web page", "url")
		return nil
	}
	return res
}

// importImage fetches a remote image and stores it as an upload owned by
// userID, applying the same type and size checks as direct uploads.
func (s *Server) importImage(ctx context.Context, userID, rawURL string) (map[string]any, error) {
	res, err := s.fetcher.Get(ctx, rawURL, "image/png,image/jpeg,image/gif,image/webp", fetch.MaxImage)
	if err != nil {
		return nil, err
	}
	if len(res.Body) > maxUpload {
		return nil, fetch.ErrTooLarge
	}
	// Trust the bytes, not the remote Content-Type; SVG is never accepted.
	ct := http.DetectContentType(res.Body)
	if !allowedImages[ct] {
		return nil, errUnsupportedImage
	}
	id, err := s.store.CreateUpload(ctx, userID, ct, len(res.Body))
	if err != nil {
		return nil, localErr{err}
	}
	if err := os.WriteFile(s.uploadPath(id), res.Body, 0o640); err != nil {
		_ = s.store.DeleteUpload(ctx, id)
		return nil, localErr{err}
	}
	return map[string]any{"id": id, "url": uploadURL(id), "content_type": ct, "size": len(res.Body)}, nil
}

var errUnsupportedImage = errors.New("unsupported image type")

// localErr marks failures on our side (database, disk) rather than the remote site's.
type localErr struct{ error }

func (e localErr) Unwrap() error { return e.error }

func clipText(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// handleImportPreview fetches a link page and proposes blocks for it.
func (s *Server) handleImportPreview(w http.ResponseWriter, r *http.Request) {
	if !s.importLimit.allow(userFrom(r).ID) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "import limit reached, try again later", "")
		return
	}
	raw, ok := remoteURL(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	res := s.fetchHTML(ctx, w, raw)
	if res == nil {
		return
	}
	p := linkimport.Parse(res.URL, res.Body)
	writeJSON(w, http.StatusOK, map[string]any{
		"source_url": res.URL.String(),
		"parser":     p.Parser,
		"title":      p.Title,
		"bio":        p.Bio,
		"avatar_url": p.AvatarURL,
		"items":      linkimport.Items(p),
	})
}

// handleImportImage copies a remote image (such as an imported avatar) into
// an upload owned by the current user.
func (s *Server) handleImportImage(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if !s.uploadLimit.allow(u.ID) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "upload limit reached, try again later", "")
		return
	}
	raw, ok := remoteURL(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	up, err := s.importImage(ctx, u.ID, raw)
	switch {
	case errors.Is(err, errUnsupportedImage):
		writeErr(w, http.StatusUnprocessableEntity, "unsupported", "that image isn't a PNG, JPEG, GIF or WebP", "url")
	case errors.As(err, new(localErr)):
		s.serverErr(w, r, err)
	case err != nil:
		s.fetchErr(w, err)
	default:
		writeJSON(w, http.StatusCreated, up)
	}
}

// handleLinkPreview reads a destination's Open Graph data. The image, if
// any, is stored as an upload owned by the user so pages never hotlink.
func (s *Server) handleLinkPreview(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if !s.previewLimit.allow(u.ID) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "preview limit reached, try again later", "")
		return
	}
	raw, ok := remoteURL(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	res := s.fetchHTML(ctx, w, raw)
	if res == nil {
		return
	}
	doc := htmlmeta.Parse(res.URL, res.Body)
	out := map[string]any{
		"url":         res.URL.String(),
		"title":       clipText(doc.Title, 80),
		"description": clipText(doc.Description, 120),
	}
	if doc.Image != "" {
		switch {
		case !s.uploadLimit.allow(u.ID):
			out["image_error"] = "upload limit reached, so the image was skipped"
		default:
			up, err := s.importImage(ctx, u.ID, doc.Image)
			switch {
			case errors.Is(err, errUnsupportedImage):
				out["image_error"] = "the page's image isn't a PNG, JPEG, GIF or WebP"
			case err != nil:
				s.log.Info("link preview image skipped", "err", err)
				out["image_error"] = "couldn't download the page's image"
			default:
				out["thumbnail_upload_id"] = up["id"]
				out["thumbnail_url"] = up["url"]
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}
