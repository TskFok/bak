// Package baidutoken 是百度令牌的唯一写入口，命令行和本地面板都通过 Save 落盘。
package baidutoken

import (
	"errors"
	"time"

	"BaiduYunPanBak/global"
	"BaiduYunPanBak/internal/cloudacct"
	"BaiduYunPanBak/model"
	"BaiduYunPanBak/utils/cache"
)

// 测试里替换这两个变量，避免连接真实的 MySQL 和 Redis。
var (
	cacheSetter = defaultCacheSetter
	tokenWriter = defaultTokenWriter
)

func defaultCacheSetter(key, value string, ttl int) {
	if global.RedisClient == nil {
		return
	}
	cache.Set(key, value, ttl)
}

// defaultTokenWriter 更新 tokens 表里的唯一一行，不存在时新建。
func defaultTokenWriter(t model.Token) error {
	if global.DataBase == nil {
		return errors.New("数据库未初始化")
	}
	now := model.LocalTime(time.Now())
	cur := model.Token{}.Find()
	if cur.Id != 0 {
		ok := model.Token{}.Update(
			map[string]interface{}{
				"access_token":  t.AccessToken,
				"refresh_token": t.RefreshToken,
				"expires_in":    t.ExpiresIn,
				"updated_at":    time.Time(now),
			},
			map[string]interface{}{"id": cur.Id},
		)
		if !ok {
			return errors.New("更新 tokens 表失败")
		}
		return nil
	}
	if t.CreatedAt == nil {
		created := now
		t.CreatedAt = &created
	}
	t.UpdatedAt = &now
	if !t.Create(&t) {
		return errors.New("写入 tokens 表失败")
	}
	return nil
}

// Save 写 tokens 表和 Redis access_token（过期时间为 expiresIn 秒）。
// accountSaver 非空时，另外把加密后的百度密钥交给它保存。
func Save(access, refresh string, expiresIn int, accountSaver func(blob string) error) error {
	if access == "" {
		return errors.New("access token 为空")
	}
	if err := tokenWriter(model.Token{AccessToken: access, RefreshToken: refresh, ExpiresIn: int32(expiresIn)}); err != nil {
		return err
	}
	cacheSetter("access_token", access, expiresIn)
	if accountSaver == nil {
		return nil
	}
	blob, err := cloudacct.Seal(global.JwtSecret, cloudacct.Secrets{
		ClientID:     global.BaiduYunClientId,
		ClientSecret: global.BaiduYunClientSecret,
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresAt:    time.Now().Add(time.Duration(expiresIn) * time.Second),
	})
	if err != nil {
		return err
	}
	return accountSaver(blob)
}
