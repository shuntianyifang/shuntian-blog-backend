package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"shuntian.blog/backend/internal/domain"
	"shuntian.blog/backend/internal/repository"
)

type Service struct{ Store *repository.Store }

func Token() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func Hash(value string) string { h := sha256.Sum256([]byte(value)); return hex.EncodeToString(h[:]) }
func (s *Service) Save(ctx context.Context, id int64, d domain.Draft) (domain.Post, error) {
	if err := domain.Validate(d); err != nil {
		return domain.Post{}, err
	}
	html, err := domain.Render(d.Markdown)
	if err != nil {
		return domain.Post{}, err
	}
	return s.Store.Save(ctx, id, d, html)
}
func (s *Service) Snapshot(ctx context.Context, key string, req domain.SnapshotRequest) (domain.Snapshot, error) {
	if len(key) < 8 || len(key) > 128 || req.Selections == nil || len(req.Selections) > 1000 {
		return domain.Snapshot{}, domain.ErrInvalid
	}
	sort.Slice(req.Selections, func(i, j int) bool { return req.Selections[i].PostID < req.Selections[j].PostID })
	for i, sel := range req.Selections {
		if sel.PostID <= 0 || sel.RevisionID <= 0 || (i > 0 && sel.PostID == req.Selections[i-1].PostID) {
			return domain.Snapshot{}, domain.ErrInvalid
		}
	}
	body, _ := json.Marshal(req)
	return s.Store.CreateSnapshot(ctx, key, Hash(string(body)), Token(), req.Selections)
}
