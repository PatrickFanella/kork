package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"git.subcult.tv/subculture-collective/zelda/internal/blocks"
	"git.subcult.tv/subculture-collective/zelda/internal/catalog"
	"git.subcult.tv/subculture-collective/zelda/internal/customcss"
	"git.subcult.tv/subculture-collective/zelda/internal/fonts"
	"git.subcult.tv/subculture-collective/zelda/internal/plans"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

// Importing pages from a Kork account export (#40). The ZIP is uploaded,
// read in memory and checked; nothing from it is trusted. Each chosen page
// is recreated with new IDs through the same validation, sanitizing and
// screening as the editor. Sign-up contacts, stats, custom domains and
// account fields are never imported.

// exportFormatVersion is written to account.json. Exports from before the
// field have no version and are read as version 1.
const exportFormatVersion = 1

const (
	// maxImportZip bounds the uploaded export.
	maxImportZip = 64 << 20
	// maxImportManifest bounds account.json once decompressed.
	maxImportManifest = 16 << 20
	maxImportEntries  = 5000
)

var importUploadName = regexp.MustCompile(`^uploads/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.[a-z0-9]{1,6}$`)

// importErr is a problem with the uploaded export as a whole.
type importErr struct {
	status int
	code   string
	msg    string
}

func (e *importErr) Error() string { return e.msg }

// importedExport is a checked export: its manifest and its ZIP entries.
type importedExport struct {
	acct  exportAccount
	files map[string]*zip.File
}

// readExport opens and checks an export ZIP.
func readExport(data []byte) (*importedExport, error) {
	bad := func(msg string) error { return &importErr{http.StatusUnprocessableEntity, "invalid_export", msg} }
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, bad("That file isn't a ZIP export.")
	}
	if len(zr.File) > maxImportEntries {
		return nil, bad("The export has too many files.")
	}
	out := &importedExport{files: map[string]*zip.File{}}
	for _, f := range zr.File {
		name := f.Name
		if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, `\`) || path.Clean(name) != name ||
			strings.HasPrefix(name, "../") || name == ".." || !utf8.ValidString(name) {
			return nil, bad("The export contains a file with an unsafe name.")
		}
		if _, dup := out.files[name]; dup {
			return nil, bad("The export contains the same file twice.")
		}
		out.files[name] = f
	}
	mf := out.files["account.json"]
	if mf == nil {
		return nil, bad("The ZIP has no account.json, so it isn't a Kork export.")
	}
	raw, err := readZipEntry(mf, maxImportManifest)
	if err != nil {
		return nil, bad("account.json is too large or unreadable.")
	}
	var head struct {
		FormatVersion int `json:"format_version"`
	}
	if json.Unmarshal(raw, &head) != nil {
		return nil, bad("account.json isn't valid JSON.")
	}
	if head.FormatVersion > exportFormatVersion || head.FormatVersion < 0 {
		return nil, &importErr{http.StatusUnprocessableEntity, "unsupported_version",
			fmt.Sprintf("This export uses format version %d, which this server can't read yet.", head.FormatVersion)}
	}
	if err := json.Unmarshal(raw, &out.acct); err != nil {
		return nil, bad("account.json isn't a Kork export.")
	}
	return out, nil
}

// readZipEntry reads one entry, refusing more than max bytes whatever the
// header claims, so a ZIP bomb can't expand in memory.
func readZipEntry(f *zip.File, max int64) ([]byte, error) {
	if f.UncompressedSize64 > uint64(max) {
		return nil, errors.New("entry too large")
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, errors.New("entry too large")
	}
	return data, nil
}

// readImportZip reads the uploaded export from the "file" field.
func (s *Server) readImportZip(w http.ResponseWriter, r *http.Request) (*importedExport, bool) {
	// The upload can be large; allow it more time than ordinary requests.
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(5 * time.Minute))
	r.Body = http.MaxBytesReader(w, r.Body, maxImportZip+1<<20)
	file, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid", "attach your export ZIP in the file field", "file")
		return nil, false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxImportZip+1))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid", "could not read the upload", "file")
		return nil, false
	}
	if len(data) > maxImportZip {
		writeErr(w, http.StatusRequestEntityTooLarge, "too_large", "exports up to 64 MB can be imported", "file")
		return nil, false
	}
	exp, err := readExport(data)
	var ie *importErr
	if errors.As(err, &ie) {
		writeErr(w, ie.status, ie.code, ie.msg, "file")
		return nil, false
	} else if err != nil {
		s.serverErr(w, r, err)
		return nil, false
	}
	return exp, true
}

type importPreviewPage struct {
	Index         int    `json:"index"`
	Slug          string `json:"slug"`
	Title         string `json:"title"`
	Blocks        int    `json:"blocks"`
	SlugAvailable bool   `json:"slug_available"`
}

// handleImportExportPreview lists the pages in an export.
func (s *Server) handleImportExportPreview(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if !s.acct.importLimit.allow(u.ID) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "you can import 10 times an hour", "")
		return
	}
	exp, ok := s.readImportZip(w, r)
	if !ok {
		return
	}
	out := []importPreviewPage{}
	for i, p := range exp.acct.Pages {
		if p == nil || p.Page == nil {
			continue
		}
		pv := importPreviewPage{Index: i, Slug: p.Slug, Title: p.Title, Blocks: len(p.Blocks)}
		if slugProblem(p.Slug) == "" {
			if _, err := s.store.PageBySlug(r.Context(), p.Slug); errors.Is(err, store.ErrNotFound) {
				pv.SlugAvailable = true
			}
		}
		out = append(out, pv)
	}
	writeJSON(w, http.StatusOK, map[string]any{"pages": out})
}

// importSkip is something from the export that wasn't recreated.
type importSkip struct {
	Page   string `json:"page"`
	What   string `json:"what"`
	Reason string `json:"reason"`
}

// handleImportExport recreates chosen pages from an export. The "pages"
// form field is JSON: [{"index": 0, "slug": "new-name"}].
func (s *Server) handleImportExport(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if !s.acct.importLimit.allow(u.ID) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "you can import 10 times an hour", "")
		return
	}
	exp, ok := s.readImportZip(w, r)
	if !ok {
		return
	}
	var picks []struct {
		Index int    `json:"index"`
		Slug  string `json:"slug"`
	}
	if err := json.Unmarshal([]byte(r.FormValue("pages")), &picks); err != nil || len(picks) == 0 || len(picks) > 50 {
		writeErr(w, http.StatusUnprocessableEntity, "invalid", "choose which pages to import", "pages")
		return
	}
	seen := map[string]bool{}
	for _, pk := range picks {
		pk.Slug = strings.ToLower(strings.TrimSpace(pk.Slug))
		if pk.Index < 0 || pk.Index >= len(exp.acct.Pages) || exp.acct.Pages[pk.Index] == nil || exp.acct.Pages[pk.Index].Page == nil {
			writeErr(w, http.StatusUnprocessableEntity, "invalid", "that page isn't in the export", "pages")
			return
		}
		if p := slugProblem(pk.Slug); p != "" {
			writeErr(w, http.StatusUnprocessableEntity, "invalid_slug", pk.Slug+": "+p, "pages")
			return
		}
		if seen[pk.Slug] {
			writeErr(w, http.StatusUnprocessableEntity, "invalid_slug", "each imported page needs its own name", "pages")
			return
		}
		seen[pk.Slug] = true
	}
	limits, ok := s.limitsFor(w, r)
	if !ok {
		return
	}
	created := []map[string]string{}
	skipped := []importSkip{}
	for _, pk := range picks {
		slug := strings.ToLower(strings.TrimSpace(pk.Slug))
		p, skips, err := s.importPage(r.Context(), u, exp, exp.acct.Pages[pk.Index], slug, limits)
		var ie *importErr
		switch {
		case errors.As(err, &ie):
			skipped = append(skipped, importSkip{Page: slug, What: "page", Reason: ie.msg})
			continue
		case err != nil:
			s.serverErr(w, r, err)
			return
		}
		skipped = append(skipped, skips...)
		created = append(created, map[string]string{"id": p.ID, "slug": p.Slug})
	}
	writeJSON(w, http.StatusOK, map[string]any{"pages": created, "skipped": skipped})
}

// importPage recreates one exported page as an unpublished page named slug.
func (s *Server) importPage(ctx context.Context, u *store.User, exp *importedExport, ep *exportPage, slug string,
	limits plans.Limits) (*store.Page, []importSkip, error) {
	var skips []importSkip
	skip := func(what, reason string) { skips = append(skips, importSkip{Page: slug, What: what, Reason: reason}) }

	// Copy the uploads this page refers to, re-checked as new uploads.
	pageJSON, _ := json.Marshal(ep)
	idMap := map[string]string{}
	for _, up := range exp.acct.Uploads {
		if up == nil || !uuidRe.MatchString(up.ID) || !bytes.Contains(pageJSON, []byte(up.ID)) {
			continue
		}
		newID, reason := s.importUpload(ctx, u.ID, exp, up, limits.Features[plans.CustomFonts])
		if reason != "" {
			skip("file "+up.File, reason)
		}
		idMap[up.ID] = newID
	}
	remap := func(s string) string {
		for old, nu := range idMap {
			s = strings.ReplaceAll(s, old, nu)
		}
		return s
	}

	title := strings.Join(strings.Fields(ep.Title), " ")
	if title == "" || utf8.RuneCountInString(title) > 80 {
		title = slug
	}
	owns := func(id string) bool { return uuidRe.MatchString(id) && s.store.OwnsUpload(ctx, u.ID, id) }
	var theme catalog.Theme
	_ = json.Unmarshal([]byte(remap(string(ep.Theme))), &theme)
	theme, _, err := theme.Normalize()
	if err != nil {
		theme = catalog.DefaultTheme()
		skip("theme", "The theme couldn't be read, so the default theme is used.")
	}
	// The same ownership checks as PATCH /api/pages/{id}: a theme may only
	// point at the importer's own uploads, and fonts at their own font files.
	if id := theme.Background.ImageUploadID; id != "" && !owns(id) {
		theme.Background.ImageUploadID = ""
		if theme.Background.Kind == "image" {
			theme.Background.Kind = "solid"
			skip("theme", "The background image wasn't imported, so a solid background is used.")
		}
	}
	if theme.Background.Kind != "image" {
		theme.Background.ImageUploadID = ""
	}
	if fu := theme.FontUploads; fu != nil {
		for _, id := range []*string{&fu.Body, &fu.Display} {
			if *id != "" && !s.ownsFont(ctx, u.ID, *id) {
				*id = ""
				skip("theme", "An uploaded font wasn't imported, so the preset font is used.")
			}
		}
		if fu.Body == "" && fu.Display == "" {
			theme.FontUploads = nil
		}
	}
	themeRaw, _ := json.Marshal(theme)
	p, err := s.store.CreatePageLimit(ctx, u.ID, slug, title, themeRaw, limits.Pages)
	switch {
	case errors.Is(err, store.ErrConflict):
		return nil, nil, &importErr{code: "slug_taken", msg: "The name " + slug + " is already taken."}
	case errors.Is(err, store.ErrLimit):
		return nil, nil, &importErr{code: "limit", msg: fmt.Sprintf("Your plan allows %d pages.", limits.Pages)}
	case err != nil:
		return nil, nil, err
	}

	if bio := strings.TrimSpace(ep.Bio); utf8.RuneCountInString(bio) <= 300 {
		p.Bio = bio
	}
	if ep.AvatarUploadID != nil {
		if id := idMap[*ep.AvatarUploadID]; id != "" {
			p.AvatarUploadID = &id
		}
	}
	if atprotoDIDRe.MatchString(ep.AtprotoDID) {
		p.AtprotoDID = ep.AtprotoDID
	}
	if wallet, err := normalizeWallet(ep.MonetizationWallet); err == nil && wallet != "" {
		if bad, _ := s.unsafeURLs(ctx, []string{wallet}); len(bad) == 0 {
			p.MonetizationWallet = wallet
		} else {
			skip("Web Monetization wallet", "The wallet address is on a phishing or malware blocklist.")
		}
	}
	if err := s.store.UpdatePage(ctx, p); err != nil {
		return nil, nil, err
	}

	// Custom CSS goes through the same limits as PUT /api/pages/{id}/css.
	if css := strings.TrimSpace(remap(ep.CustomCSS)); len(css) > customcss.MaxInput {
		skip("custom CSS", fmt.Sprintf("The custom CSS is larger than %d KB, so it was left out.", customcss.MaxInput>>10))
	} else if css != "" {
		res := customcss.Sanitize(css, customcss.UploadChecker(owns))
		if len(res.CSS) > customcss.MaxOutput {
			skip("custom CSS", "The custom CSS is too large after scoping, so it was left out.")
		} else {
			if err := s.store.SetPageCSS(ctx, p.ID, res.CSS); err != nil {
				return nil, nil, err
			}
			if len(res.Warnings) > 0 {
				skip("custom CSS", "Some custom CSS was removed by the sanitizer.")
			}
		}
	}

	for i, b := range ep.Blocks {
		if b == nil {
			continue
		}
		what := fmt.Sprintf("block %d (%s)", i+1, b.Type)
		raw, _, err := blocks.TakeSecrets(b.Type, json.RawMessage(remap(string(b.Data))))
		if err != nil {
			skip(what, "Its data couldn't be read.")
			continue
		}
		if b.Type == "email_signup" {
			// Provider keys aren't in exports, and the visitors agreed to the
			// original list, so an imported form keeps its list in Kork.
			var d map[string]any
			if json.Unmarshal(raw, &d) == nil && d["destination"] != nil && d["destination"] != blocks.DestKork {
				d["destination"] = blocks.DestKork
				raw, _ = json.Marshal(d)
				skip(what, "Sign-ups now stay in Kork. Reconnect Brevo or listmonk in the editor to send them there.")
			}
		}
		data, err := blocks.Validate(b.Type, raw, owns)
		if err != nil {
			skip(what, err.Error())
			continue
		}
		hits, err := s.blockHits(ctx, []*store.Block{{Type: b.Type, Data: data}})
		if err != nil {
			s.log.Warn("import screening incomplete; allowing unflagged URLs", "err", err)
		}
		if len(hits) > 0 {
			skip(what, blockedMessage(hits[0].URL))
			continue
		}
		sched := b.Schedule
		if sched.StartsAt != nil && sched.EndsAt != nil && !sched.EndsAt.After(*sched.StartsAt) {
			sched = store.Schedule{}
		}
		if _, err := s.store.CreateBlock(ctx, p.ID, b.Type, data, b.Enabled, sched); errors.Is(err, store.ErrLimit) {
			skip(what, fmt.Sprintf("A page holds up to %d blocks.", store.MaxBlocks))
			break
		} else if err != nil {
			return nil, nil, err
		}
	}
	if n := len(ep.SignupContacts); n > 0 {
		skip("sign-up contacts", fmt.Sprintf("%d addresses weren't imported: they agreed to the original page's list.", n))
	}
	if n := len(ep.Domains); n > 0 {
		skip("custom domains", "Custom domains aren't imported. Add them again in the Domains tab.")
	}
	return p, skips, nil
}

// importUpload copies one upload from the export as a new upload owned by
// userID. It returns the new ID, or "" and the reason it was left out.
// ownsFont reports whether id is a font upload owned by userID.
func (s *Server) ownsFont(ctx context.Context, userID, id string) bool {
	if !uuidRe.MatchString(id) {
		return false
	}
	up, err := s.store.Upload(ctx, id)
	return err == nil && up.UserID == userID && fonts.IsFont(up.ContentType)
}

// importUpload copies one file from the export as a new upload. Fonts are
// only copied when allowFonts is set, as font uploads need Pro.
func (s *Server) importUpload(ctx context.Context, userID string, exp *importedExport, up *exportUpload, allowFonts bool) (string, string) {
	f := exp.files[up.File]
	if f == nil || !importUploadName.MatchString(up.File) {
		return "", "The file is missing from the export."
	}
	if !s.uploadLimit.allow(userID) {
		return "", "The hourly upload limit was reached. Upload it again later."
	}
	data, err := readZipEntry(f, maxUpload)
	if err != nil {
		return "", "The file is larger than 5 MB."
	}
	ct := http.DetectContentType(data)
	if !allowedImages[ct] {
		ct = fonts.Sniff(data)
		if ct == "" || len(data) > fonts.MaxSize {
			return "", "The file isn't a supported image or font."
		}
		if !allowFonts {
			return "", "Font uploads are part of Pro, so the font was left out."
		}
	}
	id, err := s.store.CreateUpload(ctx, userID, ct, len(data))
	if err != nil {
		return "", "The file couldn't be saved."
	}
	if err := os.WriteFile(s.uploadPath(id), data, 0o640); err != nil {
		_ = s.store.DeleteUpload(ctx, id)
		return "", "The file couldn't be saved."
	}
	return id, ""
}
