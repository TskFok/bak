package cloudacct

import (
	"encoding/json"
	"net/url"
	"time"

	"BaiduYunPanBak/internal/crypto"
)

type Secrets struct {
	ClientID     string    `json:"client_id,omitempty"`
	ClientSecret string    `json:"client_secret,omitempty"`
	AccessToken  string    `json:"access_token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	DeviceID     string    `json:"device_id,omitempty"`
	URL          string    `json:"url,omitempty"`
	Username     string    `json:"username,omitempty"`
	Password     string    `json:"password,omitempty"`
}

func Seal(jwtSecret string, s Secrets) (string, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return crypto.Encrypt(jwtSecret, string(raw))
}

func Open(jwtSecret, blob string) (Secrets, error) {
	plain, err := crypto.Decrypt(jwtSecret, blob)
	if err != nil {
		return Secrets{}, err
	}
	var s Secrets
	err = json.Unmarshal([]byte(plain), &s)
	return s, err
}

func BaiduAuthorizeURL(clientID, deviceID string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", "oob")
	q.Set("scope", "basic,netdisk")
	q.Set("device_id", deviceID)
	return "https://openapi.baidu.com/oauth/2.0/authorize?" + q.Encode()
}

func AliyunAuthorizeURL(clientID string) string {
	q := url.Values{}
	q.Set("client_id", clientID)
	q.Set("redirect_uri", "oob")
	q.Set("scope", "user:base,file:all:read,file:all:write")
	q.Set("response_type", "code")
	return "https://openapi.alipan.com/oauth/authorize?" + q.Encode()
}

func PublicView(provider, status, lastError string) map[string]string {
	return map[string]string{"provider": provider, "status": status, "lastError": lastError}
}
