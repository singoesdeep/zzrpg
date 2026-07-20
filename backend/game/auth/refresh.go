package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/singoesdeep/zzrpg/backend/engine/store"
)

// RefreshStore persists refresh tokens by hash so sessions are revocable and
// refresh tokens are single-use (rotated on every refresh).
type RefreshStore interface {
	// Save records a refresh token hash for a user with an expiry.
	Save(ctx context.Context, tokenHash string, userID int64, expiresAt time.Time) error
	// Consume atomically validates and removes a (non-expired) token, returning
	// the owning user id. It returns ErrInvalidRefreshToken if the token is
	// unknown or expired — the basis of rotation (a consumed token can't be reused).
	Consume(ctx context.Context, tokenHash string) (userID int64, err error)
	// Delete revokes a token (logout); revoking an unknown token is not an error.
	Delete(ctx context.Context, tokenHash string) error
}

// newRefreshToken returns a random opaque token and its hash. Only the hash is
// ever stored; the raw token is shown to the client once.
func newRefreshToken() (raw, hash string, err error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", "", err
	}
	raw = hex.EncodeToString(b[:])
	return raw, hashToken(raw), nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// --- in-memory store (default; single-process) -----------------------------

type memRefreshStore struct {
	mu     sync.Mutex
	tokens map[string]memRefresh
}

type memRefresh struct {
	userID    int64
	expiresAt time.Time
}

func newMemRefreshStore() *memRefreshStore {
	return &memRefreshStore{tokens: make(map[string]memRefresh)}
}

func (s *memRefreshStore) Save(_ context.Context, tokenHash string, userID int64, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[tokenHash] = memRefresh{userID: userID, expiresAt: expiresAt}
	return nil
}

func (s *memRefreshStore) Consume(_ context.Context, tokenHash string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tokens[tokenHash]
	if !ok || time.Now().After(t.expiresAt) {
		delete(s.tokens, tokenHash)
		return 0, ErrInvalidRefreshToken
	}
	delete(s.tokens, tokenHash)
	return t.userID, nil
}

func (s *memRefreshStore) Delete(_ context.Context, tokenHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, tokenHash)
	return nil
}

// --- Postgres store --------------------------------------------------------

type pgRefreshStore struct {
	db store.Store
}

// NewPgRefreshStore returns a Postgres-backed, multi-node RefreshStore.
func NewPgRefreshStore(db store.Store) RefreshStore {
	return &pgRefreshStore{db: db}
}

func (s *pgRefreshStore) Save(ctx context.Context, tokenHash string, userID int64, expiresAt time.Time) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO refresh_tokens (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`,
		tokenHash, userID, expiresAt,
	)
	return err
}

func (s *pgRefreshStore) Consume(ctx context.Context, tokenHash string) (int64, error) {
	var userID int64
	err := s.db.QueryRow(ctx,
		`DELETE FROM refresh_tokens WHERE token_hash = $1 AND expires_at > now() RETURNING user_id`,
		tokenHash,
	).Scan(&userID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, ErrInvalidRefreshToken
		}
		return 0, err
	}
	return userID, nil
}

func (s *pgRefreshStore) Delete(ctx context.Context, tokenHash string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM refresh_tokens WHERE token_hash = $1`, tokenHash)
	return err
}
