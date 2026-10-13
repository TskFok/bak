package baidutoken

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"bak/global"
	"bak/internal/cloudacct"
	"bak/internal/store"
	"bak/model"
	"bak/tool/openapi"
)

var errNoDB = errors.New("数据库未初始化")

var tokenReader = defaultTokenReader

// tokenRefresher 用 refresh_token 向百度换新令牌。测试里替换，避免访问真实接口。
var tokenRefresher = defaultTokenRefresher

func defaultTokenReader() (model.Token, error) {
	if global.DataBase == nil {
		return model.Token{}, errNoDB
	}
	var rows []model.Token
	if err := global.DataBase.Order("id").Limit(1).Find(&rows).Error; err != nil {
		return model.Token{}, err
	}
	if len(rows) == 0 {
		return model.Token{}, nil
	}
	return rows[0], nil
}

type tokenCred struct {
	access  string
	refresh string
	exp     time.Time
	known   bool
}

// Reconcile 比较面板账号和 token 表里的百度令牌，以到期时间较晚的一份为准。
// 应用 Key、应用密钥和设备 ID 留在面板账号里。令牌相同则不改写。
func Reconcile(cloud cloudacct.Secrets, row model.Token, now time.Time) (cloudacct.Secrets, model.Token, bool, bool) {
	winner := preferToken(cloudCred(cloud), rowCred(row))
	nextCloud := cloud
	nextRow := row
	cloudChanged := false
	rowChanged := false

	if nextCloud.AccessToken != winner.access || nextCloud.RefreshToken != winner.refresh {
		nextCloud.AccessToken = winner.access
		nextCloud.RefreshToken = winner.refresh
		if winner.known {
			nextCloud.ExpiresAt = winner.exp
		}
		cloudChanged = true
	} else if winner.known && winner.access != "" && nextCloud.ExpiresAt.IsZero() {
		nextCloud.ExpiresAt = winner.exp
		cloudChanged = true
	}

	if nextRow.AccessToken != winner.access || nextRow.RefreshToken != winner.refresh {
		nextRow.AccessToken = winner.access
		nextRow.RefreshToken = winner.refresh
		nextRow.ExpiresIn = expiresInSeconds(winner.exp, winner.known, now)
		rowChanged = true
	}
	return nextCloud, nextRow, cloudChanged, rowChanged
}

func cloudCred(s cloudacct.Secrets) tokenCred {
	c := tokenCred{access: s.AccessToken, refresh: s.RefreshToken, exp: s.ExpiresAt}
	if s.AccessToken != "" && !s.ExpiresAt.IsZero() {
		c.known = true
	}
	return c
}

func rowCred(row model.Token) tokenCred {
	c := tokenCred{access: row.AccessToken, refresh: row.RefreshToken}
	if exp, ok := rowExpiry(row); ok {
		c.exp = exp
		c.known = true
	}
	return c
}

func rowExpiry(row model.Token) (time.Time, bool) {
	if row.AccessToken == "" {
		return time.Time{}, false
	}
	var base time.Time
	switch {
	case row.UpdatedAt != nil:
		base = time.Time(*row.UpdatedAt)
	case row.CreatedAt != nil:
		base = time.Time(*row.CreatedAt)
	default:
		return time.Time{}, false
	}
	return base.Add(time.Duration(row.ExpiresIn) * time.Second), true
}

// preferToken 在两边都有令牌时选到期更晚的一份。无法比较时保留面板账号里的令牌。
func preferToken(cloud, row tokenCred) tokenCred {
	if cloud.access == "" && row.access == "" {
		return tokenCred{}
	}
	if cloud.access == "" {
		return row
	}
	if row.access == "" {
		return cloud
	}
	if cloud.access == row.access && cloud.refresh == row.refresh {
		if !cloud.known && row.known {
			cloud.exp = row.exp
			cloud.known = true
		}
		return cloud
	}
	if cloud.known && row.known && row.exp.After(cloud.exp) {
		return row
	}
	if row.known && !cloud.known {
		return row
	}
	return cloud
}

func expiresInSeconds(exp time.Time, known bool, now time.Time) int32 {
	if !known || !exp.After(now) {
		return 0
	}
	sec := int64(exp.Sub(now) / time.Second)
	if sec > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(sec)
}

// Sync 读取 token 表并与面板里的百度密钥对齐，把落后的一行写回 token 表。
// 两边都已过期时先向百度刷新，不再沿用旧令牌。
// 面板密钥有变化时 changed 为 true，由调用方保存。数据库未初始化时原样返回。
func Sync(cloud cloudacct.Secrets) (cloudacct.Secrets, bool, error) {
	row, err := tokenReader()
	if errors.Is(err, errNoDB) {
		return cloud, false, nil
	}
	if err != nil {
		return cloud, false, err
	}
	now := time.Now()
	nextCloud, nextRow, cloudChanged, rowChanged := Reconcile(cloud, row, now)
	if bothSidesExpired(cloud, row, now) {
		refreshed, err := tokenRefresher(nextCloud)
		if err != nil {
			return cloud, false, err
		}
		nextCloud.AccessToken = refreshed.AccessToken
		nextCloud.RefreshToken = refreshed.RefreshToken
		nextCloud.ExpiresAt = refreshed.ExpiresAt
		nextRow.AccessToken = refreshed.AccessToken
		nextRow.RefreshToken = refreshed.RefreshToken
		nextRow.ExpiresIn = expiresInSeconds(refreshed.ExpiresAt, !refreshed.ExpiresAt.IsZero(), now)
		cloudChanged, rowChanged = true, true
	}
	if rowChanged {
		if err := tokenWriter(nextRow); err != nil {
			return cloud, false, err
		}
	}
	if nextCloud.AccessToken != "" && (cloudChanged || rowChanged) {
		if ttl := cacheTTL(nextCloud, now); ttl > 0 {
			cacheSetter("access_token", nextCloud.AccessToken, ttl)
		}
	}
	return nextCloud, cloudChanged, nil
}

// bothSidesExpired 表示面板账号和 token 表里都有令牌，且两份都已过期。
func bothSidesExpired(cloud cloudacct.Secrets, row model.Token, now time.Time) bool {
	cc, rc := cloudCred(cloud), rowCred(row)
	if cc.access == "" || rc.access == "" || !cc.known || !rc.known {
		return false
	}
	return !cc.exp.After(now) && !rc.exp.After(now)
}

func defaultTokenRefresher(s cloudacct.Secrets) (cloudacct.Secrets, error) {
	clientID := s.ClientID
	clientSecret := s.ClientSecret
	if clientID == "" {
		clientID = global.BaiduYunClientId
	}
	if clientSecret == "" {
		clientSecret = global.BaiduYunClientSecret
	}
	if s.RefreshToken == "" || clientID == "" || clientSecret == "" {
		return s, errors.New("无法刷新百度令牌：缺少 refresh_token 或应用凭据")
	}
	client := openapi.NewAPIClient(openapi.NewConfiguration())
	resp, _, err := client.AuthApi.OauthTokenRefreshToken(context.Background()).RefreshToken(s.RefreshToken).ClientId(clientID).ClientSecret(clientSecret).Execute()
	if err != nil || resp.GetAccessToken() == "" {
		return s, refreshFailure(err)
	}
	s.AccessToken = resp.GetAccessToken()
	if rt := resp.GetRefreshToken(); rt != "" {
		s.RefreshToken = rt
	}
	s.ClientID = clientID
	s.ClientSecret = clientSecret
	if resp.GetExpiresIn() > 0 {
		s.ExpiresAt = time.Now().Add(time.Duration(resp.GetExpiresIn()) * time.Second)
	}
	return s, nil
}

// refreshFailure 只保留百度返回的状态码。传输错误里常带有完整请求地址，其中包含 refresh_token 和应用密钥。
func refreshFailure(err error) error {
	var apiErr openapi.GenericOpenAPIError
	if err != nil && errors.As(err, &apiErr) && apiErr.Error() != "" {
		return fmt.Errorf("刷新百度令牌失败: %s", apiErr.Error())
	}
	return errors.New("刷新百度令牌失败")
}

func cacheTTL(s cloudacct.Secrets, now time.Time) int {
	if s.ExpiresAt.IsZero() || !s.ExpiresAt.After(now) {
		return 0
	}
	sec := int(s.ExpiresAt.Sub(now) / time.Second)
	if sec < 1 {
		return 0
	}
	return sec
}

// Current 读取面板百度账号和 token 表，对齐后返回同一份令牌。
func Current() (cloudacct.Secrets, error) {
	cloud, err := readBaiduCloud()
	if err != nil {
		return cloudacct.Secrets{}, err
	}
	next, changed, err := Sync(cloud)
	if err != nil {
		return cloudacct.Secrets{}, err
	}
	if changed {
		if err := writeBaiduCloud(next); err != nil {
			return next, err
		}
	}
	return next, nil
}

func readBaiduCloud() (cloudacct.Secrets, error) {
	if global.DataBase == nil {
		return cloudacct.Secrets{}, nil
	}
	clouds, err := store.NewGorm(global.DataBase).ListClouds(context.Background())
	if err != nil {
		return cloudacct.Secrets{}, err
	}
	for _, c := range clouds {
		if c.Provider != "baidu" || c.SecretBlob == "" {
			continue
		}
		return cloudacct.Open(global.JwtSecret, c.SecretBlob)
	}
	return cloudacct.Secrets{}, nil
}

func writeBaiduCloud(s cloudacct.Secrets) error {
	if global.DataBase == nil {
		return errNoDB
	}
	st := store.NewGorm(global.DataBase)
	ctx := context.Background()
	acct := store.CloudAccount{Provider: "baidu", DisplayName: "百度网盘", Status: "connected"}
	clouds, err := st.ListClouds(ctx)
	if err != nil {
		return err
	}
	for _, c := range clouds {
		if c.Provider != "baidu" {
			continue
		}
		acct.DisplayName = c.DisplayName
		acct.Status = c.Status
		if acct.Status == "" || acct.Status == "disconnected" {
			acct.Status = "connected"
		}
		if old, err := cloudacct.Open(global.JwtSecret, c.SecretBlob); err == nil {
			if s.DeviceID == "" {
				s.DeviceID = old.DeviceID
			}
			if s.ClientID == "" {
				s.ClientID, s.ClientSecret = old.ClientID, old.ClientSecret
			}
		}
		break
	}
	if s.ClientID == "" {
		s.ClientID = global.BaiduYunClientId
		s.ClientSecret = global.BaiduYunClientSecret
	}
	blob, err := cloudacct.Seal(global.JwtSecret, s)
	if err != nil {
		return err
	}
	acct.SecretBlob = blob
	return st.UpsertCloud(ctx, &acct)
}
