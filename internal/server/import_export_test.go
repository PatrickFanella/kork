package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"git.subcult.tv/subculture-collective/zelda/internal/screen"
	"github.com/jackc/pgx/v5/pgxpool"
)

// postExport sends an export ZIP to an import endpoint.
func (c *client) postExport(path string, zipData []byte, pages any, out any) int {
	c.e.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "export.zip")
	fw.Write(zipData) //nolint:errcheck
	if pages != nil {
		b, _ := json.Marshal(pages)
		mw.WriteField("pages", string(b)) //nolint:errcheck
	}
	mw.Close()
	req, _ := http.NewRequest("POST", c.e.ts.URL+path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Requested-With", "zelda")
	res, err := c.c.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil {
		json.NewDecoder(res.Body).Decode(out) //nolint:errcheck
	}
	return res.StatusCode
}

func zipOf(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write(data) //nolint:errcheck
	}
	zw.Close()
	return buf.Bytes()
}

// Export → import recreates a page without contacts, stats or domains (#40).
func TestImportPageFromExport(t *testing.T) {
	e := setup(t)
	pool := testPool(t)
	fake := &screen.Fake{}
	e.srv.SetScreener(fake)
	alice, pageID := livePage(t, e, "alice@example.com", "alice")
	img := uploadPNG(t, e, alice)
	if code := alice.do("PATCH", "/api/pages/"+pageID, map[string]any{"bio": "Prints and zines", "avatar_upload_id": img,
		"theme": map[string]any{"colors": map[string]any{"accent": "#ff3366"}}}, nil); code != 200 {
		t.Fatal("profile")
	}
	for _, b := range []struct {
		typ  string
		data map[string]any
	}{
		{"link", map[string]any{"title": "Shop", "url": "https://shop.example"}},
		{"image", map[string]any{"upload_id": img, "alt": "A print"}},
		{"link", map[string]any{"title": "Later flagged", "url": "https://soon-bad.example/x"}},
		{"email_signup", map[string]any{"title": "News"}},
	} {
		if code, _, ae := alice.createBlock(pageID, b.typ, b.data); code != 201 {
			t.Fatalf("%s: %d %+v", b.typ, code, ae)
		}
	}
	var blk struct{ ID string }
	pool.QueryRow(context.Background(), `SELECT id FROM blocks WHERE page_id = $1 AND type = 'email_signup'`, pageID).Scan(&blk.ID) //nolint:errcheck
	e.subscribeAndConfirm(blk.ID, "fan@example.org")
	if _, err := pool.Exec(context.Background(), `INSERT INTO domains (page_id, hostname, verify_token) VALUES ($1, 'links.alice.example', 't')`, pageID); err != nil {
		t.Fatal(err)
	}

	code, zipBody, _ := alice.get("/api/account/export")
	if code != 200 {
		t.Fatalf("export: %d", code)
	}
	export := []byte(zipBody)
	if !bytes.Contains(export, []byte(`"format_version": 1`)) {
		// account.json is deflated; read it to check the version.
		zr, _ := zip.NewReader(bytes.NewReader(export), int64(len(export)))
		for _, f := range zr.File {
			if f.Name == "account.json" {
				data, _ := readZipEntry(f, maxImportManifest)
				if !bytes.Contains(data, []byte(`"format_version": 1`)) {
					t.Fatal("export has no format_version")
				}
			}
		}
	}
	// Flag one link after the export, as if it became a phishing page.
	fake.Flag("soon-bad.example", "SOCIAL_ENGINEERING")
	// Let the safe verdict cached when the link was saved expire.
	if _, err := pool.Exec(context.Background(), `DELETE FROM screen_verdicts`); err != nil {
		t.Fatal(err)
	}

	bob := e.client()
	bob.signupVerified("bob@example.com")
	var preview struct {
		Pages []importPreviewPage `json:"pages"`
	}
	if code := bob.postExport("/api/import/export/preview", export, nil, &preview); code != 200 || len(preview.Pages) != 1 ||
		preview.Pages[0].Slug != "alice" || preview.Pages[0].SlugAvailable || preview.Pages[0].Blocks != 4 {
		t.Fatalf("preview: %d %+v", code, preview)
	}
	var res struct {
		Pages   []map[string]string `json:"pages"`
		Skipped []importSkip        `json:"skipped"`
	}
	if code := bob.postExport("/api/import/export", export, []map[string]any{{"index": 0, "slug": "bob-copy"}}, &res); code != 200 || len(res.Pages) != 1 {
		t.Fatalf("import: %d %+v", code, res)
	}
	reasons := ""
	for _, s := range res.Skipped {
		reasons += s.What + ": " + s.Reason + "\n"
	}
	for _, want := range []string{"blocklist", "sign-up contacts", "Custom domains aren't imported"} {
		if !strings.Contains(reasons, want) {
			t.Errorf("skipped report is missing %q:\n%s", want, reasons)
		}
	}

	newID := res.Pages[0]["id"]
	var page struct {
		Published      bool    `json:"published"`
		Bio            string  `json:"bio"`
		AvatarUploadID *string `json:"avatar_upload_id"`
		Theme          struct {
			Colors struct{ Accent string } `json:"colors"`
		} `json:"theme"`
		Blocks []struct {
			Type string         `json:"type"`
			Data map[string]any `json:"data"`
		} `json:"blocks"`
		Domains []any `json:"domains"`
	}
	bob.do("GET", "/api/pages/"+newID, nil, &page)
	if page.Published || page.Bio != "Prints and zines" || page.Theme.Colors.Accent != "#ff3366" || len(page.Domains) != 0 {
		t.Fatalf("imported page: %+v", page)
	}
	if page.AvatarUploadID == nil || *page.AvatarUploadID == img || !e.srv.store.OwnsUpload(context.Background(), userIDOf(t, pool, "bob@example.com"), *page.AvatarUploadID) {
		t.Fatalf("avatar wasn't copied to a new upload owned by bob: %v", page.AvatarUploadID)
	}
	if len(page.Blocks) != 3 || page.Blocks[1].Type != "image" || page.Blocks[1].Data["upload_id"] != *page.AvatarUploadID {
		t.Fatalf("blocks: %+v", page.Blocks)
	}
	var contacts, stats int
	pool.QueryRow(context.Background(), `SELECT count(*) FROM signup_contacts c JOIN blocks b ON b.id = c.block_id WHERE b.page_id = $1`, newID).Scan(&contacts) //nolint:errcheck
	pool.QueryRow(context.Background(), `SELECT count(*) FROM stats_daily WHERE page_id = $1`, newID).Scan(&stats)                                               //nolint:errcheck
	if contacts != 0 || stats != 0 {
		t.Fatalf("import created %d contacts and %d stats rows", contacts, stats)
	}
	// A taken name is reported, not overwritten.
	if code := bob.postExport("/api/import/export", export, []map[string]any{{"index": 0, "slug": "alice"}}, &res); code != 200 ||
		len(res.Pages) != 0 || !strings.Contains(res.Skipped[0].Reason, "already taken") {
		t.Fatalf("taken slug: %d %+v", code, res)
	}
}

func userIDOf(t *testing.T, pool *pgxpool.Pool, email string) string {
	t.Helper()
	var id string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&id) //nolint:errcheck
	return id
}

func TestImportRejectsBadArchives(t *testing.T) {
	e := setup(t)
	bob := e.client()
	bob.signupVerified("bob@example.com")
	manifest := []byte(`{"pages":[]}`)
	huge := bytes.Repeat([]byte(" "), maxImportManifest+10) // compresses to almost nothing
	for name, data := range map[string][]byte{
		"not a zip":       []byte("hello"),
		"no manifest":     zipOf(t, map[string][]byte{"readme.txt": []byte("x")}),
		"path traversal":  zipOf(t, map[string][]byte{"account.json": manifest, "../evil": []byte("x")}),
		"absolute path":   zipOf(t, map[string][]byte{"account.json": manifest, "/etc/passwd": []byte("x")}),
		"unknown version": zipOf(t, map[string][]byte{"account.json": []byte(`{"format_version":99,"pages":[]}`)}),
		"zip bomb":        zipOf(t, map[string][]byte{"account.json": huge}),
	} {
		var ae apiErr
		if code := bob.postExport("/api/import/export/preview", data, nil, &ae); code != 422 {
			t.Errorf("%s: %d %+v", name, code, ae)
		} else if name == "unknown version" && ae.Error.Code != "unsupported_version" {
			t.Errorf("unknown version: %+v", ae)
		}
	}
}
