package cloudacct

import (
	"strings"
	"testing"
	"time"
)

func TestSealOpen(t *testing.T) {
	in := Secrets{URL: "https://dav.example", Username: "u", Password: "secret-password", ExpiresAt: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)}
	blob, err := Seal("jwt", in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(blob, "secret-password") {
		t.Fatal("blob leaked password")
	}
	out, err := Open("jwt", blob)
	if err != nil {
		t.Fatal(err)
	}
	if out.Password != "secret-password" || !out.ExpiresAt.Equal(in.ExpiresAt) {
		t.Fatalf("%+v", out)
	}
}

func TestAuthorizeURL(t *testing.T) {
	b := BaiduAuthorizeURL("appkey", "appid")
	if !strings.Contains(b, "client_id=appkey") || !strings.Contains(b, "device_id=appid") || !strings.Contains(b, "redirect_uri=oob") {
		t.Fatal(b)
	}
	a := AliyunAuthorizeURL("cid")
	if !strings.Contains(a, "https://openapi.alipan.com/oauth/authorize") || !strings.Contains(a, "client_id=cid") {
		t.Fatal(a)
	}
}

func TestPublicViewHidesSecrets(t *testing.T) {
	view := PublicView("webdav", "disconnected", "认证失败")
	raw := view["provider"] + view["status"] + view["lastError"]
	if strings.Contains(raw, "password") || view["provider"] != "webdav" {
		t.Fatal(view)
	}
}
