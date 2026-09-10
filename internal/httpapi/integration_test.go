package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"shuntian.blog/backend/internal/domain"
	"shuntian.blog/backend/internal/repository"
	"shuntian.blog/backend/internal/service"
)

func TestDatabaseHTTPFlow(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not configured; run scripts/test.ps1 for required integration checks")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.Path != "/shuntian_blog_test" || parsed.Hostname() != "127.0.0.1" || parsed.User.Username() != "shuntian_blog_test_app" {
		t.Fatal("refusing non-local/non-test database")
	}
	ownerURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	ownerParsed, err := url.Parse(ownerURL)
	if err != nil || ownerParsed.Path != parsed.Path || ownerParsed.Host != parsed.Host || ownerParsed.User.Username() != "shuntian_blog_test_owner" {
		t.Fatal("refusing unsafe migration target")
	}
	ctx := context.Background()
	owner, err := repository.Open(ctx, ownerURL)
	if err != nil {
		t.Fatal("owner unavailable")
	}
	defer owner.DB.Close()
	for range 2 {
		if err = owner.Migrate(ctx, "shuntian_blog_test_app"); err != nil {
			t.Fatal("migration failed:", err)
		}
	}
	store, err := repository.Open(ctx, dsn)
	if err != nil {
		t.Fatal("app unavailable")
	}
	defer store.DB.Close()
	var actual string
	if err = store.DB.QueryRow(ctx, `SELECT current_database()`).Scan(&actual); err != nil || actual != "shuntian_blog_test" {
		t.Fatal("test target mismatch")
	}
	// Cross-database connect and runtime DDL must both fail.
	cross := *parsed
	cross.Path = "/shuntian_blog_dev"
	other, _ := repository.Open(ctx, cross.String())
	if other.DB.Ping(ctx) == nil {
		t.Fatal("test role can access development database")
	}
	other.DB.Close()
	if _, err = store.DB.Exec(ctx, `CREATE TABLE forbidden_test_table(id int)`); err == nil {
		t.Fatal("app role can create schema objects")
	}
	password := os.Getenv("TEST_ADMIN_PASSWORD")
	if len(password) < 12 {
		t.Fatal("test password required")
	}
	if _, _, err = store.Admin(ctx, "integration-admin"); err == domain.ErrNotFound {
		hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if store.CreateAdmin(ctx, "integration-admin", string(hash)) != nil {
			t.Fatal("test admin creation failed")
		}
	}
	token := service.Token()
	handler := New(&service.Service{Store: store}, Config{Origin: "http://127.0.0.1:8081", BuildToken: token})
	var cookie *http.Cookie
	var csrf string
	call := func(method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
		var data []byte
		if body != nil {
			data, _ = json.Marshal(body)
		}
		request := httptest.NewRequest(method, path, bytes.NewReader(data))
		request.RemoteAddr = "127.0.0.1:50000"
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "http://127.0.0.1:8081")
		if cookie != nil {
			request.AddCookie(cookie)
		}
		if csrf != "" {
			request.Header.Set("X-CSRF-Token", csrf)
		}
		for key, value := range headers {
			request.Header.Set(key, value)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	expect := func(response *httptest.ResponseRecorder, status int) {
		t.Helper()
		if response.Code != status {
			t.Fatalf("status=%d want=%d body=%s", response.Code, status, response.Body.String())
		}
	}
	expect(call("GET", "/api/health/ready", nil, nil), 200)
	expect(call("GET", "/api/v1/admin/posts", nil, nil), 401)
	expect(call("POST", "/api/v1/auth/login", map[string]string{"username": "integration-admin", "password": password}, map[string]string{"Origin": "https://evil.invalid"}), 403)
	expect(call("POST", "/api/v1/auth/login", map[string]string{"username": "integration-admin", "password": "incorrect"}, nil), 401)
	login := call("POST", "/api/v1/auth/login", map[string]string{"username": "integration-admin", "password": password}, nil)
	expect(login, 200)
	cookie = login.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/api/" {
		t.Fatal("unsafe session cookie")
	}
	var identity map[string]string
	json.Unmarshal(login.Body.Bytes(), &identity)
	csrf = identity["csrf_token"]
	expect(call("GET", "/api/v1/auth/me", nil, nil), 200)
	marker := "PRIVATE-DRAFT-" + service.Token()
	slug := "test-" + service.Token()[:12]
	draft := domain.Draft{Slug: slug, Title: "集成验收文章", Summary: "公开摘要", Markdown: "## 公共正文\n\nPUBLIC-VERIFIED\n", PublishedAt: time.Now().UTC(), Category: domain.Taxon{Slug: "testing", Name: "验收"}, Tags: []domain.Taxon{{Slug: "go", Name: "Go"}}}
	expect(call("POST", "/api/v1/admin/posts", draft, map[string]string{"X-CSRF-Token": "wrong"}), 403)
	create := call("POST", "/api/v1/admin/posts", draft, nil)
	expect(create, 201)
	var post domain.Post
	json.Unmarshal(create.Body.Bytes(), &post)
	path := "/api/v1/admin/posts/" + jsonNumber(post.ID)
	expect(call("POST", path+"/preview", map[string]string{"markdown": "<script>bad()</script>\n\npreview-safe"}, nil), 200)
	selections := domain.SnapshotRequest{Selections: []domain.Selection{{PostID: post.ID, RevisionID: post.RevisionID}}}
	key := service.Token()
	snap := call("POST", "/api/v1/admin/snapshots", selections, map[string]string{"Idempotency-Key": key})
	expect(snap, 201)
	var result map[string]string
	json.Unmarshal(snap.Body.Bytes(), &result)
	snapshotID := result["snapshot_id"]
	same := call("POST", "/api/v1/admin/snapshots", selections, map[string]string{"Idempotency-Key": key})
	expect(same, 201)
	if same.Body.String() != snap.Body.String() {
		t.Fatal("idempotency changed result")
	}
	expect(call("POST", "/api/v1/admin/snapshots", domain.SnapshotRequest{Selections: []domain.Selection{}}, map[string]string{"Idempotency-Key": key}), 409)
	draft.BaseRevisionID = post.RevisionID
	draft.Markdown = marker
	updated := call("PATCH", path, draft, nil)
	expect(updated, 200)
	expect(call("PATCH", path, draft, nil), 409)
	buildPath := "/api/v1/build/snapshots/" + snapshotID
	expect(call("GET", buildPath, nil, nil), 401)
	exported := call("GET", buildPath, nil, map[string]string{"Authorization": "Bearer " + token})
	expect(exported, 200)
	for _, secret := range []string{marker, password, token, csrf, "password_hash", "markdown", "base_revision_id"} {
		if strings.Contains(exported.Body.String(), secret) {
			t.Fatal("sensitive data leaked")
		}
	}
	if !strings.Contains(exported.Body.String(), "PUBLIC-VERIFIED") {
		t.Fatal("frozen body changed")
	}
	if _, err = owner.DB.Exec(ctx, `UPDATE post_revisions SET title='bad' WHERE id=$1`, post.RevisionID); err == nil {
		t.Fatal("revision mutation accepted")
	}
	if _, err = owner.DB.Exec(ctx, `DELETE FROM publication_snapshots WHERE id=$1`, snapshotID); err == nil {
		t.Fatal("snapshot deletion accepted")
	}
	// Concurrent requests with one key must produce one snapshot.
	svc := &service.Service{Store: store}
	concurrentKey := service.Token()
	ids := make(chan string, 4)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, e := svc.Snapshot(ctx, concurrentKey, domain.SnapshotRequest{Selections: []domain.Selection{{PostID: post.ID, RevisionID: post.RevisionID}}})
			if e != nil {
				ids <- "error"
			} else {
				ids <- value.ID
			}
		}()
	}
	wg.Wait()
	close(ids)
	first := ""
	for id := range ids {
		if id == "error" {
			t.Fatal("concurrent snapshot failed")
		}
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatal("duplicate concurrent snapshot")
		}
	}
	expect(call("POST", "/api/v1/auth/logout", map[string]string{}, nil), 200)
	expect(call("GET", "/api/v1/admin/posts", nil, nil), 401)
	// Fixed peer IP, not spoofable forwarding headers, controls the rate limit.
	for range 10 {
		_ = call("POST", "/api/v1/auth/login", map[string]string{"username": "unknown", "password": "incorrect"}, map[string]string{"X-Forwarded-For": service.Token()})
	}
	expect(call("POST", "/api/v1/auth/login", map[string]string{"username": "unknown", "password": "incorrect"}, nil), 429)
}
func jsonNumber(id int64) string { body, _ := json.Marshal(id); return string(body) }
