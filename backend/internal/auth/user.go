package auth

import (
	"context"
	"errors"
	"time"
)

var (
	ErrUserAlreadyExists   = errors.New("username or email already registered")
	ErrUserNotFound        = errors.New("user not found")
	ErrInvalidCredentials  = errors.New("invalid username or password")
	ErrTooManyAttempts     = errors.New("too many failed login attempts; try again later")
	ErrInvalidRefreshToken = errors.New("invalid or expired refresh token")
)

// Role values for RBAC.
const (
	RolePlayer = "player"
	RoleAdmin  = "admin"
)

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type UserRepository interface {
	Create(ctx context.Context, user *User) error
	GetByUsername(ctx context.Context, username string) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	GetByID(ctx context.Context, id int64) (*User, error)
}
