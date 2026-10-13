package baidutoken

import (
	"errors"
	"strings"
	"testing"
	"time"

	"bak/global"
	"bak/internal/cloudacct"
	"bak/model"
)

type calls struct {
	cacheKey, cacheVal string
	cacheTTL           int
	cacheN             int
	tokens             []model.Token
}

func stub(t *testing.T) *calls {
	t.Helper()
	c := &calls{}
	oldCache, oldTok := cacheSetter, tokenWriter
	oldSecret, oldID, oldCS := global.JwtSecret, global.BaiduYunClientId, global.BaiduYunClientSecret
	cacheSetter = func(key, value string, ttl int) {
		c.cacheKey, c.cacheVal, c.cacheTTL = key, value, ttl
		c.cacheN++
	}
	tokenWriter = func(tk model.Token) error {
		c.tokens = append(c.tokens, tk)
		return nil
	}
	global.JwtSecret = "jwt-secret"
	global.BaiduYunClientId, global.BaiduYunClientSecret = "app-key", "app-secret"
	t.Cleanup(func() {
		cacheSetter, tokenWriter = oldCache, oldTok
		global.JwtSecret, global.BaiduYunClientId, global.BaiduYunClientSecret = oldSecret, oldID, oldCS
	})
	return c
}

func TestSaveWritesTokenCacheAndSealedAccount(t *testing.T) {
	c := stub(t)
	var blob string
	before := time.Now()
	err := Save("access-1", "refresh-1", 2592000, func(b string) error { blob = b; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if c.cacheN != 1 || c.cacheKey != "access_token" || c.cacheVal != "access-1" || c.cacheTTL != 2592000 {
		t.Fatalf("cache: %+v", c)
	}
	if len(c.tokens) != 1 {
		t.Fatalf("tokens: %+v", c.tokens)
	}
	tk := c.tokens[0]
	if tk.AccessToken != "access-1" || tk.RefreshToken != "refresh-1" || tk.ExpiresIn != 2592000 {
		t.Fatalf("token: %+v", tk)
	}
	if blob == "" || strings.Contains(blob, "access-1") {
		t.Fatal("blob empty or leaked token")
	}
	sec, err := cloudacct.Open("jwt-secret", blob)
	if err != nil {
		t.Fatal(err)
	}
	if sec.AccessToken != "access-1" || sec.RefreshToken != "refresh-1" || sec.ClientID != "app-key" || sec.ClientSecret != "app-secret" {
		t.Fatalf("secrets: %+v", sec)
	}
	if d := sec.ExpiresAt.Sub(before); d < 2591990*time.Second || d > 2592010*time.Second {
		t.Fatalf("expiresAt: %v", sec.ExpiresAt)
	}
}

func TestSaveNilAccountSaver(t *testing.T) {
	c := stub(t)
	if err := Save("a", "r", 60, nil); err != nil {
		t.Fatal(err)
	}
	if c.cacheN != 1 || len(c.tokens) != 1 {
		t.Fatalf("%+v", c)
	}
}

func TestSaveAccountSaverError(t *testing.T) {
	stub(t)
	want := errors.New("db down")
	if err := Save("a", "r", 60, func(string) error { return want }); !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
}

func TestSaveTokenWriteErrorSkipsCache(t *testing.T) {
	c := stub(t)
	tokenWriter = func(model.Token) error { return errors.New("boom") }
	if err := Save("a", "r", 60, nil); err == nil {
		t.Fatal("want error")
	}
	if c.cacheN != 0 {
		t.Fatal("cache written despite token failure")
	}
}

func TestSaveRejectsEmptyAccess(t *testing.T) {
	c := stub(t)
	if err := Save("", "r", 60, nil); err == nil {
		t.Fatal("want error")
	}
	if c.cacheN != 0 || len(c.tokens) != 0 {
		t.Fatal("wrote empty token")
	}
}
