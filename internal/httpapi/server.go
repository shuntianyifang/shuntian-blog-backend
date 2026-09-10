package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"shuntian.blog/backend/internal/domain"
	"shuntian.blog/backend/internal/service"
)

type Config struct {
	Origin, BuildToken string
	Secure             bool
}
type attempt struct {
	Count int
	Until time.Time
}
type Server struct {
	Service  *service.Service
	Config   Config
	mu       sync.Mutex
	attempts map[string]attempt
	dummy    []byte
}

func New(s *service.Service, c Config) http.Handler {
	hash, _ := bcrypt.GenerateFromPassword([]byte(service.Token()), bcrypt.DefaultCost)
	server := &Server{Service: s, Config: c, attempts: map[string]attempt{}, dummy: hash}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health/live", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /api/health/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if s.Store.DB.Ping(ctx) != nil {
			fail(w, 503, "database_unavailable")
			return
		}
		respond(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /api/v1/auth/login", server.login)
	mux.HandleFunc("GET /api/v1/auth/me", server.auth(func(w http.ResponseWriter, r *http.Request) {
		_, u, csrf, _ := server.session(r)
		respond(w, 200, map[string]string{"username": u, "csrf_token": csrf})
	}))
	mux.HandleFunc("POST /api/v1/auth/logout", server.auth(func(w http.ResponseWriter, r *http.Request) {
		hash, _, _, _ := server.session(r)
		if err := s.Store.DeleteSession(r.Context(), hash); err != nil {
			serverError(w, err)
			return
		}
		server.cookie(w, "", -1)
		respond(w, 200, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("GET /api/v1/admin/posts", server.auth(server.list))
	mux.HandleFunc("POST /api/v1/admin/posts", server.auth(server.save))
	mux.HandleFunc("GET /api/v1/admin/posts/{id}", server.auth(server.get))
	mux.HandleFunc("PATCH /api/v1/admin/posts/{id}", server.auth(server.save))
	mux.HandleFunc("POST /api/v1/admin/posts/{id}/preview", server.auth(server.preview))
	mux.HandleFunc("POST /api/v1/admin/snapshots", server.auth(server.snapshot))
	mux.HandleFunc("GET /api/v1/build/snapshots/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !equal(r.Header.Get("Authorization"), "Bearer "+c.BuildToken) {
			fail(w, 401, "unauthorized")
			return
		}
		snapshot, err := s.Store.Snapshot(r.Context(), r.PathValue("id"))
		if err != nil {
			serverError(w, err)
			return
		}
		respond(w, 200, snapshot)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Request-ID", service.Token()[:16])
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}
func equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, code string) {
	respond(w, status, map[string]any{"error": map[string]string{"code": code, "request_id": w.Header().Get("X-Request-ID")}})
}
func serverError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrConflict):
		fail(w, 409, "conflict")
	case errors.Is(err, domain.ErrNotFound):
		fail(w, 404, "not_found")
	case errors.Is(err, domain.ErrInvalid):
		fail(w, 400, "invalid_input")
	default:
		fail(w, 500, "internal_error")
	}
}
func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		fail(w, 415, "json_required")
		return false
	}
	reader := http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil {
		fail(w, 400, "invalid_json")
		return false
	}
	if decoder.Decode(new(any)) != io.EOF {
		fail(w, 400, "invalid_json")
		return false
	}
	return true
}
func (s *Server) cookie(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: "blog_session", Value: token, Path: "/api/", HttpOnly: true, Secure: s.Config.Secure, SameSite: http.SameSiteStrictMode, MaxAge: maxAge})
}
func (s *Server) session(r *http.Request) (string, string, string, error) {
	cookie, err := r.Cookie("blog_session")
	if err != nil || len(cookie.Value) != 64 {
		return "", "", "", domain.ErrNotFound
	}
	hash := service.Hash(cookie.Value)
	user, csrf, err := s.Service.Store.Session(r.Context(), hash)
	return hash, user, csrf, err
}
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _, csrf, err := s.session(r)
		if err != nil {
			fail(w, 401, "unauthorized")
			return
		}
		if r.Method != "GET" && (!equal(r.Header.Get("Origin"), s.Config.Origin) || !equal(r.Header.Get("X-CSRF-Token"), csrf)) {
			fail(w, 403, "csrf_rejected")
			return
		}
		next(w, r)
	}
}
func (s *Server) allowLogin(remote string) bool {
	ip, _, _ := net.SplitHostPort(remote)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for key, value := range s.attempts {
		if now.After(value.Until) {
			delete(s.attempts, key)
		}
	}
	a, ok := s.attempts[ip]
	if !ok {
		if len(s.attempts) >= 256 {
			return false
		}
		a = attempt{Until: now.Add(5 * time.Minute)}
	}
	a.Count++
	s.attempts[ip] = a
	return a.Count <= 10
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !equal(r.Header.Get("Origin"), s.Config.Origin) {
		fail(w, 403, "origin_rejected")
		return
	}
	if !s.allowLogin(r.RemoteAddr) {
		w.Header().Set("Retry-After", "300")
		fail(w, 429, "rate_limited")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.Username) > 100 || len(req.Password) > 72 {
		fail(w, 401, "invalid_credentials")
		return
	}
	id, hash, err := s.Service.Store.Admin(r.Context(), req.Username)
	if err != nil {
		_ = bcrypt.CompareHashAndPassword(s.dummy, []byte(req.Password))
		fail(w, 401, "invalid_credentials")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		fail(w, 401, "invalid_credentials")
		return
	}
	token, csrf := service.Token(), service.Token()
	if err = s.Service.Store.CreateSession(r.Context(), service.Hash(token), id, csrf); err != nil {
		serverError(w, err)
		return
	}
	s.cookie(w, token, 43200)
	respond(w, 200, map[string]string{"username": req.Username, "csrf_token": csrf})
}
func postID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		fail(w, 400, "invalid_id")
		return 0, false
	}
	return id, true
}
func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	offset := 0
	var err error
	if value := r.URL.Query().Get("offset"); value != "" {
		offset, err = strconv.Atoi(value)
	}
	if err != nil || offset < 0 || offset > 1000000 {
		fail(w, 400, "invalid_offset")
		return
	}
	posts, err := s.Service.Store.Posts(r.Context(), offset)
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, 200, map[string]any{"posts": posts, "offset": offset, "limit": 50})
}
func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	id, ok := postID(w, r)
	if !ok {
		return
	}
	post, err := s.Service.Store.Post(r.Context(), id)
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, 200, post)
}
func (s *Server) save(w http.ResponseWriter, r *http.Request) {
	var id int64
	var ok bool
	if r.Method == "PATCH" {
		id, ok = postID(w, r)
		if !ok {
			return
		}
	}
	var draft domain.Draft
	if !decode(w, r, &draft) {
		return
	}
	post, err := s.Service.Save(r.Context(), id, draft)
	if err != nil {
		serverError(w, err)
		return
	}
	status := 200
	if id == 0 {
		status = 201
	}
	respond(w, status, post)
}
func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	id, ok := postID(w, r)
	if !ok {
		return
	}
	if _, err := s.Service.Store.Post(r.Context(), id); err != nil {
		serverError(w, err)
		return
	}
	var req struct {
		Markdown string `json:"markdown"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.Markdown) > 200000 {
		fail(w, 400, "invalid_input")
		return
	}
	html, err := domain.Render(req.Markdown)
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, 200, map[string]string{"html": html})
}
func (s *Server) snapshot(w http.ResponseWriter, r *http.Request) {
	var req domain.SnapshotRequest
	if !decode(w, r, &req) {
		return
	}
	snapshot, err := s.Service.Snapshot(r.Context(), r.Header.Get("Idempotency-Key"), req)
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, 201, map[string]string{"snapshot_id": snapshot.ID, "status": "snapshot_created"})
}
