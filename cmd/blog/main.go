package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
	"shuntian.blog/backend/internal/httpapi"
	"shuntian.blog/backend/internal/repository"
	"shuntian.blog/backend/internal/service"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Operation failed:", err)
		os.Exit(1)
	}
}
func run() error {
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	if command != "serve" && command != "migrate" && command != "admin" {
		return errors.New("usage: blog [serve|migrate|admin USERNAME]")
	}
	variable := "DATABASE_URL"
	if command == "migrate" {
		variable = "MIGRATION_DATABASE_URL"
	}
	dsn := os.Getenv(variable)
	if dsn == "" {
		return fmt.Errorf("%s is required", variable)
	}
	ctx := context.Background()
	store, err := repository.Open(ctx, dsn)
	if err != nil {
		return errors.New("database configuration invalid")
	}
	defer store.DB.Close()
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = store.DB.Ping(pingCtx)
	cancel()
	if err != nil {
		return errors.New("database unavailable; check local credentials and service")
	}
	if command == "migrate" {
		if err = store.Migrate(ctx, os.Getenv("APP_ROLE")); err != nil {
			return errors.New("migration failed; verify schema ownership, version and app role")
		}
		fmt.Println("Migrations applied and verified.")
		return nil
	}
	if command == "admin" {
		if len(os.Args) != 3 || strings.TrimSpace(os.Args[2]) == "" || len(os.Args[2]) > 100 {
			return errors.New("usage: blog admin USERNAME")
		}
		password := []byte(os.Getenv("ADMIN_PASSWORD"))
		if len(password) == 0 {
			fmt.Print("Administrator password (12-72 bytes): ")
			password, err = term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Println()
			if err != nil {
				return errors.New("use an interactive terminal")
			}
		}
		if len(password) < 12 || len(password) > 72 {
			return errors.New("password must be 12-72 bytes")
		}
		hash, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
		clear(password)
		if err != nil {
			return errors.New("password hashing failed")
		}
		if err = store.CreateAdmin(ctx, os.Args[2], string(hash)); err != nil {
			return errors.New("admin creation failed; a single administrator is allowed")
		}
		fmt.Println("Administrator created.")
		return nil
	}
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return errors.New("LISTEN_ADDR must be a loopback address")
	}
	origin := os.Getenv("PUBLIC_ORIGIN")
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("PUBLIC_ORIGIN must be an exact http(s) origin without trailing slash")
	}
	secure := os.Getenv("COOKIE_SECURE") != "false"
	if !secure && (parsed.Scheme != "http" || net.ParseIP(parsed.Hostname()) == nil || !net.ParseIP(parsed.Hostname()).IsLoopback()) {
		return errors.New("insecure cookies are allowed only for explicit loopback HTTP development")
	}
	if secure && parsed.Scheme != "https" {
		return errors.New("secure cookies require an HTTPS public origin")
	}
	token := os.Getenv("BUILD_TOKEN")
	if len(token) < 32 {
		return errors.New("BUILD_TOKEN must contain at least 32 random characters")
	}
	server := &http.Server{Addr: addr, Handler: httpapi.New(&service.Service{Store: store}, httpapi.Config{Origin: origin, BuildToken: token, Secure: secure}), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	stop, cleanup := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cleanup()
	go func() {
		<-stop.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	fmt.Println("Blog API listening on", addr)
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return errors.New("HTTP server failed to start")
}
