package auth

import (
	"context"
	"errors"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

var ErrUserExists = errors.New("已经创建过本地账号")
var ErrBadLogin = errors.New("用户名或密码错误")

type User struct {
	ID           uint
	Username     string
	PasswordHash string
}

type UserStore interface {
	Count(ctx context.Context) (int64, error)
	Create(ctx context.Context, user *User) error
	FindByUsername(ctx context.Context, username string) (User, error)
}

type FieldError struct {
	Fields map[string]string
}

func (e *FieldError) Error() string { return "字段校验失败" }

func Setup(ctx context.Context, store UserStore, username, password, confirm string) (User, error) {
	fields := map[string]string{}
	if username == "" || utf8.RuneCountInString(username) > 32 {
		fields["username"] = "请填写 1 到 32 个字符的用户名"
	}
	if len(password) < 8 {
		fields["password"] = "密码至少 8 位"
	}
	if password != confirm {
		fields["confirm"] = "两次输入的密码不一致"
	}
	if len(fields) > 0 {
		return User{}, &FieldError{Fields: fields}
	}
	n, err := store.Count(ctx)
	if err != nil {
		return User{}, err
	}
	if n > 0 {
		return User{}, ErrUserExists
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	user := &User{Username: username, PasswordHash: string(hash)}
	if err := store.Create(ctx, user); err != nil {
		return User{}, err
	}
	return *user, nil
}

func Login(ctx context.Context, store UserStore, username, password string) (User, error) {
	user, err := store.FindByUsername(ctx, username)
	if err != nil {
		return User{}, ErrBadLogin
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return User{}, ErrBadLogin
	}
	return user, nil
}
