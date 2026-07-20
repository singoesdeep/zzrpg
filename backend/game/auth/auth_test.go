package auth

import (
	"context"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

type mockUserRepository struct {
	users  map[string]*User
	emails map[string]*User
}

func newMockUserRepository() *mockUserRepository {
	return &mockUserRepository{
		users:  make(map[string]*User),
		emails: make(map[string]*User),
	}
}

func (m *mockUserRepository) Create(ctx context.Context, user *User) error {
	if _, ok := m.users[user.Username]; ok {
		return ErrUserAlreadyExists
	}
	if _, ok := m.emails[user.Email]; ok {
		return ErrUserAlreadyExists
	}
	user.ID = int64(len(m.users) + 1)
	m.users[user.Username] = user
	m.emails[user.Email] = user
	return nil
}

func (m *mockUserRepository) GetByUsername(ctx context.Context, username string) (*User, error) {
	u, ok := m.users[username]
	if !ok {
		return nil, ErrUserNotFound
	}
	return u, nil
}

func (m *mockUserRepository) GetByEmail(ctx context.Context, email string) (*User, error) {
	u, ok := m.emails[email]
	if !ok {
		return nil, ErrUserNotFound
	}
	return u, nil
}

func (m *mockUserRepository) GetByID(ctx context.Context, id int64) (*User, error) {
	for _, u := range m.users {
		if u.ID == id {
			return u, nil
		}
	}
	return nil, ErrUserNotFound
}

func TestRegister(t *testing.T) {
	repo := newMockUserRepository()
	service := NewAuthService(repo, "secret")

	// 1. Success case
	user, err := service.Register(context.Background(), "player1", "player1@test.com", "password")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if user.Username != "player1" || user.Email != "player1@test.com" {
		t.Errorf("unexpected user data: %+v", user)
	}

	if user.PasswordHash == "password" || len(user.PasswordHash) == 0 {
		t.Errorf("password was not hashed: %s", user.PasswordHash)
	}

	// 2. Duplicate case
	_, err = service.Register(context.Background(), "player1", "player2@test.com", "password")
	if err != ErrUserAlreadyExists {
		t.Errorf("expected ErrUserAlreadyExists, got %v", err)
	}
}

func TestLogin(t *testing.T) {
	repo := newMockUserRepository()
	jwtSecret := "secretkeyfortest"
	service := NewAuthService(repo, jwtSecret)

	_, err := service.Register(context.Background(), "player1", "player1@test.com", "correctpassword")
	if err != nil {
		t.Fatalf("registration failed: %v", err)
	}

	// 1. Login success
	pair, err := service.Login(context.Background(), "player1", "correctpassword")
	if err != nil {
		t.Fatalf("expected successful login, got %v", err)
	}

	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatal("expected non-empty access and refresh tokens")
	}

	// Verify token claims
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(pair.AccessToken, claims, func(token *jwt.Token) (interface{}, error) {
		return []byte(jwtSecret), nil
	})

	if err != nil || !token.Valid {
		t.Fatalf("token validation failed: %v", err)
	}

	if claims.Username != "player1" || claims.UserID != 1 {
		t.Errorf("invalid claims data: %+v", claims)
	}

	// 2. Login invalid password
	_, err = service.Login(context.Background(), "player1", "wrongpassword")
	if err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}

	// 3. Login invalid user
	_, err = service.Login(context.Background(), "nonexistent", "correctpassword")
	if err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials for unknown user, got %v", err)
	}
}

// TestLoginBruteForceLockout proves the login limiter locks a username after
// repeated failures, so even the correct password is refused until the window
// passes.
func TestLoginBruteForceLockout(t *testing.T) {
	repo := newMockUserRepository()
	service := NewAuthService(repo, "secret")
	if _, err := service.Register(context.Background(), "victim", "victim@test.com", "correctpassword"); err != nil {
		t.Fatalf("register: %v", err)
	}

	// defaultLoginMaxFailures wrong attempts, each an invalid-credentials error.
	for i := 0; i < defaultLoginMaxFailures; i++ {
		if _, err := service.Login(context.Background(), "victim", "wrong"); err != ErrInvalidCredentials {
			t.Fatalf("attempt %d: expected ErrInvalidCredentials, got %v", i+1, err)
		}
	}

	// Now locked: the correct password is rejected with ErrTooManyAttempts.
	if _, err := service.Login(context.Background(), "victim", "correctpassword"); err != ErrTooManyAttempts {
		t.Errorf("expected ErrTooManyAttempts after lockout, got %v", err)
	}
}

// TestRefreshRotation proves refresh tokens are single-use: refreshing returns a
// new pair, the old refresh token is then rejected, and logout revokes a token.
func TestRefreshRotation(t *testing.T) {
	repo := newMockUserRepository()
	service := NewAuthService(repo, "secret")
	if _, err := service.Register(context.Background(), "p", "p@test.com", "pw"); err != nil {
		t.Fatalf("register: %v", err)
	}

	pair, err := service.Login(context.Background(), "p", "pw")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	// Refresh rotates to a new pair.
	next, err := service.Refresh(context.Background(), pair.RefreshToken)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if next.RefreshToken == pair.RefreshToken {
		t.Error("refresh token was not rotated")
	}

	// The consumed (old) refresh token is now invalid.
	if _, err := service.Refresh(context.Background(), pair.RefreshToken); err != ErrInvalidRefreshToken {
		t.Errorf("expected ErrInvalidRefreshToken for reused token, got %v", err)
	}

	// Logout revokes the current refresh token.
	if err := service.Logout(context.Background(), next.RefreshToken); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := service.Refresh(context.Background(), next.RefreshToken); err != ErrInvalidRefreshToken {
		t.Errorf("expected ErrInvalidRefreshToken after logout, got %v", err)
	}
}
