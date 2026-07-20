package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// Default token lifetimes: a short-lived access token limits the damage of a
// leaked JWT; a longer refresh token (rotated on use) keeps sessions alive.
const (
	defaultAccessTTL  = 15 * time.Minute
	defaultRefreshTTL = 30 * 24 * time.Hour
)

// TokenPair is what Login/Refresh return: a short-lived access token plus a
// rotating refresh token.
type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64 // access-token lifetime in seconds
}

type AuthService interface {
	Register(ctx context.Context, username, email, password string) (*User, error)
	Login(ctx context.Context, username, password string) (*TokenPair, error)
	// Refresh rotates a valid refresh token into a new token pair.
	Refresh(ctx context.Context, refreshToken string) (*TokenPair, error)
	// Logout revokes a refresh token.
	Logout(ctx context.Context, refreshToken string) error
}

type authService struct {
	repo       UserRepository
	jwtSecret  []byte
	limiter    *loginLimiter
	refresh    RefreshStore
	accessTTL  time.Duration
	refreshTTL time.Duration
}

type Claims struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// Option configures an authService.
type Option func(*authService)

// WithRefreshStore replaces the default in-memory refresh store (e.g. with the
// Postgres-backed store for multi-node, restart-durable sessions).
func WithRefreshStore(rs RefreshStore) Option { return func(s *authService) { s.refresh = rs } }

// WithTokenTTLs overrides the access and refresh token lifetimes.
func WithTokenTTLs(access, refresh time.Duration) Option {
	return func(s *authService) {
		if access > 0 {
			s.accessTTL = access
		}
		if refresh > 0 {
			s.refreshTTL = refresh
		}
	}
}

func NewAuthService(repo UserRepository, jwtSecret string, opts ...Option) AuthService {
	s := &authService{
		repo:       repo,
		jwtSecret:  []byte(jwtSecret),
		limiter:    newLoginLimiter(defaultLoginMaxFailures, defaultLoginLockout),
		refresh:    newMemRefreshStore(),
		accessTTL:  defaultAccessTTL,
		refreshTTL: defaultRefreshTTL,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *authService) Register(ctx context.Context, username, email, password string) (*User, error) {
	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	user := &User{
		Username:     username,
		Email:        email,
		PasswordHash: string(hashedPassword),
	}

	if err := s.repo.Create(ctx, user); err != nil {
		return nil, err
	}

	return user, nil
}

func (s *authService) Login(ctx context.Context, username, password string) (*TokenPair, error) {
	// Brute-force guard: block further attempts once a username has accumulated
	// too many recent failures, regardless of whether the password is now right.
	key := strings.ToLower(strings.TrimSpace(username))
	if s.limiter.locked(key) {
		return nil, ErrTooManyAttempts
	}

	user, err := s.repo.GetByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			// Count failures for unknown usernames too, so the endpoint can't be
			// used to brute-force which usernames exist.
			s.limiter.fail(key)
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	// Compare password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		s.limiter.fail(key)
		return nil, ErrInvalidCredentials
	}
	s.limiter.success(key)

	return s.issueTokens(ctx, user)
}

// Refresh rotates a valid refresh token: the presented token is consumed
// (single-use) and a fresh pair is issued for its owner.
func (s *authService) Refresh(ctx context.Context, refreshToken string) (*TokenPair, error) {
	userID, err := s.refresh.Consume(ctx, hashToken(refreshToken))
	if err != nil {
		return nil, err
	}
	user, err := s.repo.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil, ErrInvalidRefreshToken
		}
		return nil, err
	}
	return s.issueTokens(ctx, user)
}

// Logout revokes a refresh token so it can no longer be rotated.
func (s *authService) Logout(ctx context.Context, refreshToken string) error {
	return s.refresh.Delete(ctx, hashToken(refreshToken))
}

// issueTokens mints an access JWT and a rotating refresh token for user, storing
// the refresh token's hash.
func (s *authService) issueTokens(ctx context.Context, user *User) (*TokenPair, error) {
	role := user.Role
	if role == "" {
		role = RolePlayer
	}
	claims := &Claims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.accessTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtSecret)
	if err != nil {
		return nil, err
	}

	rawRefresh, refreshHash, err := newRefreshToken()
	if err != nil {
		return nil, err
	}
	if err := s.refresh.Save(ctx, refreshHash, user.ID, time.Now().Add(s.refreshTTL)); err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: rawRefresh,
		ExpiresIn:    int64(s.accessTTL.Seconds()),
	}, nil
}
