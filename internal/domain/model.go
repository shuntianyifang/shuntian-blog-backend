package domain

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

var ErrConflict = errors.New("revision or idempotency conflict")
var ErrNotFound = errors.New("not found")
var ErrInvalid = errors.New("invalid input")
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type Taxon struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}
type Draft struct {
	Slug           string    `json:"slug"`
	Title          string    `json:"title"`
	Summary        string    `json:"summary"`
	Markdown       string    `json:"markdown"`
	PublishedAt    time.Time `json:"published_at"`
	Category       Taxon     `json:"category"`
	Tags           []Taxon   `json:"tags"`
	BaseRevisionID int64     `json:"base_revision_id"`
}
type Post struct {
	ID         int64 `json:"id"`
	RevisionID int64 `json:"revision_id"`
	Draft
	HTML string `json:"html"`
}
type PublicPost struct {
	Slug        string    `json:"slug"`
	Title       string    `json:"title"`
	Summary     string    `json:"summary"`
	HTML        string    `json:"html"`
	PublishedAt time.Time `json:"published_at"`
	Category    Taxon     `json:"category"`
	Tags        []Taxon   `json:"tags"`
}
type Selection struct {
	PostID     int64 `json:"post_id"`
	RevisionID int64 `json:"revision_id"`
}
type SnapshotRequest struct {
	Selections []Selection `json:"selections"`
}
type Snapshot struct {
	SchemaVersion int          `json:"schema_version"`
	ID            string       `json:"id"`
	CreatedAt     time.Time    `json:"created_at"`
	SiteTitle     string       `json:"site_title"`
	PageSize      int          `json:"page_size"`
	Posts         []PublicPost `json:"posts"`
}

func ValidSlug(s string) bool { return len(s) <= 100 && slugPattern.MatchString(s) }
func Validate(d Draft) error {
	if !ValidSlug(d.Slug) || strings.TrimSpace(d.Title) == "" || len(d.Title) > 300 || len(d.Summary) > 1500 || len(d.Markdown) > 200000 || d.PublishedAt.IsZero() || d.PublishedAt.Year() < 1970 || d.PublishedAt.Year() > 9999 || !ValidTaxon(d.Category) || len(d.Tags) > 20 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, tag := range d.Tags {
		if !ValidTaxon(tag) || seen[tag.Slug] {
			return ErrInvalid
		}
		seen[tag.Slug] = true
	}
	return nil
}
func ValidTaxon(t Taxon) bool {
	return ValidSlug(t.Slug) && strings.TrimSpace(t.Name) != "" && len(t.Name) <= 100
}
func Render(markdown string) (string, error) {
	var out bytes.Buffer
	if err := goldmark.New(goldmark.WithExtensions(extension.GFM)).Convert([]byte(markdown), &out); err != nil {
		return "", err
	}
	return bluemonday.UGCPolicy().Sanitize(out.String()), nil
}
