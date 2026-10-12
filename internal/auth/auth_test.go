package auth

import (
	"context"
	"errors"
	"testing"
)

type memUsers struct {
	users []User
}

func (m *memUsers) Count(ctx context.Context) (int64, error) { return int64(len(m.users)), nil }
func (m *memUsers) Create(ctx context.Context, user *User) error {
	user.ID = uint(len(m.users) + 1)
	m.users = append(m.users, *user)
	return nil
}
func (m *memUsers) FindByUsername(ctx context.Context, username string) (User, error) {
	for _, u := range m.users {
		if u.Username == username {
			return u, nil
		}
	}
	return User{}, errors.New("not found")
}

func TestSetupOnceAndLogin(t *testing.T) {
	store := &memUsers{}
	ctx := context.Background()
	if _, err := Setup(ctx, store, "me", "password1", "password1"); err != nil {
		t.Fatal(err)
	}
	if _, err := Setup(ctx, store, "other", "password1", "password1"); !errors.Is(err, ErrUserExists) {
		t.Fatalf("got %v", err)
	}
	if _, err := Login(ctx, store, "me", "wrong-password"); !errors.Is(err, ErrBadLogin) {
		t.Fatalf("got %v", err)
	}
	user, err := Login(ctx, store, "me", "password1")
	if err != nil || user.Username != "me" {
		t.Fatalf("user %+v err %v", user, err)
	}
}

func TestSetupFieldErrors(t *testing.T) {
	store := &memUsers{}
	_, err := Setup(context.Background(), store, "", "short", "other")
	fe, ok := err.(*FieldError)
	if !ok {
		t.Fatalf("got %v", err)
	}
	if fe.Fields["username"] == "" || fe.Fields["password"] == "" || fe.Fields["confirm"] == "" {
		t.Fatalf("%v", fe.Fields)
	}
}
