package repository

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"shuntian.blog/backend/internal/domain"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct{ DB *pgxpool.Pool }

func Open(ctx context.Context, url string) (*Store, error) {
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, errors.New("invalid database configuration")
	}
	config.MaxConns = 4
	config.ConnConfig.ConnectTimeout = 5 * time.Second
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	return &Store{db}, nil
}
func (s *Store) Migrate(ctx context.Context, role string) error {
	if role != "shuntian_blog_dev_app" && role != "shuntian_blog_test_app" {
		return errors.New("unexpected app role")
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(87423101)`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(version text PRIMARY KEY, checksum text NOT NULL)`); err != nil {
		return err
	}
	entries, _ := migrations.ReadDir("migrations")
	for _, entry := range entries {
		body, _ := migrations.ReadFile("migrations/" + entry.Name())
		hash := sha256.Sum256(body)
		checksum := hex.EncodeToString(hash[:])
		var old string
		err = tx.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=$1`, entry.Name()).Scan(&old)
		if err == nil {
			if old != checksum {
				return errors.New("applied migration checksum changed")
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, err = tx.Exec(ctx, string(body)); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO schema_migrations VALUES($1,$2)`, entry.Name(), checksum); err != nil {
			return err
		}
	}
	grants := fmt.Sprintf(`GRANT USAGE ON SCHEMA public TO %s; GRANT SELECT,INSERT ON admins,post_revisions,publication_snapshots TO %s; GRANT SELECT,INSERT,UPDATE ON posts TO %s; GRANT SELECT,INSERT,DELETE ON sessions TO %s; GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA public TO %s;`, role, role, role, role, role)
	if _, err = tx.Exec(ctx, grants); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func mapError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	var e *pgconn.PgError
	if errors.As(err, &e) && e.Code == "23505" {
		return domain.ErrConflict
	}
	return err
}
func (s *Store) CreateAdmin(ctx context.Context, name, hash string) error {
	_, err := s.DB.Exec(ctx, `INSERT INTO admins(username,password_hash) VALUES($1,$2)`, name, hash)
	return mapError(err)
}
func (s *Store) Admin(ctx context.Context, name string) (int64, string, error) {
	var id int64
	var hash string
	err := s.DB.QueryRow(ctx, `SELECT id,password_hash FROM admins WHERE username=$1`, name).Scan(&id, &hash)
	return id, hash, mapError(err)
}
func (s *Store) CreateSession(ctx context.Context, hash string, id int64, csrf string) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM sessions WHERE expires_at<=now()`)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(ctx, `INSERT INTO sessions VALUES($1,$2,$3,now()+interval '12 hours')`, hash, id, csrf)
	return err
}
func (s *Store) Session(ctx context.Context, hash string) (string, string, error) {
	var user, csrf string
	err := s.DB.QueryRow(ctx, `SELECT a.username,s.csrf_token FROM sessions s JOIN admins a ON a.id=s.admin_id WHERE s.token_hash=$1 AND s.expires_at>now()`, hash).Scan(&user, &csrf)
	return user, csrf, mapError(err)
}
func (s *Store) DeleteSession(ctx context.Context, hash string) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM sessions WHERE token_hash=$1`, hash)
	return err
}

const postColumns = `p.id,r.id,p.slug,r.title,r.summary,r.markdown,r.safe_html,r.published_at,r.category,r.tags`

func scanPost(row pgx.Row) (domain.Post, error) {
	var p domain.Post
	err := row.Scan(&p.ID, &p.RevisionID, &p.Slug, &p.Title, &p.Summary, &p.Markdown, &p.HTML, &p.PublishedAt, &p.Category, &p.Tags)
	return p, mapError(err)
}
func (s *Store) Post(ctx context.Context, id int64) (domain.Post, error) {
	return scanPost(s.DB.QueryRow(ctx, `SELECT `+postColumns+` FROM posts p JOIN post_revisions r ON r.id=p.current_draft_revision_id WHERE p.id=$1`, id))
}
func (s *Store) Posts(ctx context.Context, offset int) ([]domain.Post, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+postColumns+` FROM posts p JOIN post_revisions r ON r.id=p.current_draft_revision_id ORDER BY p.id DESC LIMIT 50 OFFSET $1`, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.Post{}
	for rows.Next() {
		p, e := scanPost(rows)
		if e != nil {
			return nil, e
		}
		result = append(result, p)
	}
	return result, rows.Err()
}
func (s *Store) Save(ctx context.Context, id int64, d domain.Draft, html string) (domain.Post, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return domain.Post{}, err
	}
	defer tx.Rollback(ctx)
	if id == 0 {
		if d.BaseRevisionID != 0 {
			return domain.Post{}, domain.ErrInvalid
		}
		err = tx.QueryRow(ctx, `INSERT INTO posts(slug) VALUES($1) RETURNING id`, d.Slug).Scan(&id)
	} else {
		var base int64
		var slug string
		err = tx.QueryRow(ctx, `SELECT current_draft_revision_id,slug FROM posts WHERE id=$1 FOR UPDATE`, id).Scan(&base, &slug)
		if err == nil && (base != d.BaseRevisionID || slug != d.Slug) {
			return domain.Post{}, domain.ErrConflict
		}
	}
	if err != nil {
		return domain.Post{}, mapError(err)
	}
	if d.Tags == nil {
		d.Tags = []domain.Taxon{}
	}
	category, _ := json.Marshal(d.Category)
	tags, _ := json.Marshal(d.Tags)
	var revision int64
	err = tx.QueryRow(ctx, `INSERT INTO post_revisions(post_id,title,summary,markdown,safe_html,published_at,category,tags) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, id, d.Title, d.Summary, d.Markdown, html, d.PublishedAt, category, tags).Scan(&revision)
	if err != nil {
		return domain.Post{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE posts SET current_draft_revision_id=$2 WHERE id=$1`, id, revision); err != nil {
		return domain.Post{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Post{}, err
	}
	return domain.Post{ID: id, RevisionID: revision, Draft: d, HTML: html}, nil
}
func (s *Store) Snapshot(ctx context.Context, id string) (domain.Snapshot, error) {
	var body []byte
	var out domain.Snapshot
	err := s.DB.QueryRow(ctx, `SELECT manifest FROM publication_snapshots WHERE id=$1`, id).Scan(&body)
	if err != nil {
		return out, mapError(err)
	}
	err = json.Unmarshal(body, &out)
	return out, err
}
func (s *Store) CreateSnapshot(ctx context.Context, key, hash, id string, selections []domain.Selection) (domain.Snapshot, error) {
	var out domain.Snapshot
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	// Serialize snapshot creation so idempotency and selected immutable revisions share one transaction.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(87423102)`); err != nil {
		return out, err
	}
	var oldHash string
	var body []byte
	err = tx.QueryRow(ctx, `SELECT request_hash,manifest FROM publication_snapshots WHERE idempotency_key=$1`, key).Scan(&oldHash, &body)
	if err == nil {
		if oldHash != hash {
			return out, domain.ErrConflict
		}
		err = json.Unmarshal(body, &out)
		return out, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	out = domain.Snapshot{SchemaVersion: 1, ID: id, CreatedAt: time.Now().UTC(), SiteTitle: "顺天的博客", PageSize: 5, Posts: []domain.PublicPost{}}
	names := map[string]string{}
	for _, sel := range selections {
		p, e := scanPost(tx.QueryRow(ctx, `SELECT `+postColumns+` FROM posts p JOIN post_revisions r ON r.post_id=p.id WHERE p.id=$1 AND r.id=$2`, sel.PostID, sel.RevisionID))
		if e != nil {
			return out, e
		}
		for _, tax := range append([]domain.Taxon{p.Category}, p.Tags...) {
			if name, ok := names[tax.Slug]; ok && name != tax.Name {
				return out, domain.ErrInvalid
			}
			names[tax.Slug] = tax.Name
		}
		out.Posts = append(out.Posts, domain.PublicPost{Slug: p.Slug, Title: p.Title, Summary: p.Summary, HTML: p.HTML, PublishedAt: p.PublishedAt, Category: p.Category, Tags: p.Tags})
	}
	sort.Slice(out.Posts, func(i, j int) bool {
		a, b := out.Posts[i], out.Posts[j]
		if a.PublishedAt.Equal(b.PublishedAt) {
			return a.Slug < b.Slug
		}
		return a.PublishedAt.After(b.PublishedAt)
	})
	body, err = json.Marshal(out)
	if err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO publication_snapshots(id,idempotency_key,request_hash,manifest) VALUES($1,$2,$3,$4)`, id, key, hash, body); err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
