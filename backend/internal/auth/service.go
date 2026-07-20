package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type AuthService interface {
	Register(ctx context.Context, username, email, password string) (*User, error)
	Login(ctx context.Context, username, password string) (string, error)
}

type authService struct {
	repo      UserRepository
	jwtSecret []byte
	limiter   *loginLimiter
}

type Claims struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

func NewAuthService(repo UserRepository, jwtSecret string) AuthService {
	return &authService{
		repo:      repo,
		jwtSecret: []byte(jwtSecret),
		limiter:   newLoginLimiter(defaultLoginMaxFailures, defaultLoginLockout),
	}
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

func (s *authService) Login(ctx context.Context, username, password string) (string, error) {
	// Brute-force guard: block further attempts once a username has accumulated
	// too many recent failures, regardless of whether the password is now right.
	key := strings.ToLower(strings.TrimSpace(username))
	if s.limiter.locked(key) {
		return "", ErrTooManyAttempts
	}

	user, err := s.repo.GetByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			// Count failures for unknown usernames too, so the endpoint can't be
			// used to brute-force which usernames exist.
			s.limiter.fail(key)
			return "", ErrInvalidCredentials
		}
		return "", err
	}

	// Compare password
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err != nil {
		s.limiter.fail(key)
		return "", ErrInvalidCredentials
	}
	s.limiter.success(key)

	// Create JWT token
	expirationTime := time.Now().Add(24 * time.Hour)
	role := user.Role
	if role == "" {
		role = RolePlayer
	}
	claims := &Claims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return "", err
	}

	return tokenString, nil
}
