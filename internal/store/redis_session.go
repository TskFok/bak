package store

import (
	"context"
	"strconv"
	"time"

	"bak/global"
	"bak/internal/auth"
)

var _ auth.SessionStore = RedisSessions{}

// RedisSessions 把会话写入 Redis，键为 session:<id>，值为十进制用户 ID。
type RedisSessions struct{}

func sessionKey(id string) string { return "session:" + id }

func (r RedisSessions) Set(ctx context.Context, id string, userID uint, ttl time.Duration) error {
	return global.RedisClient.Set(ctx, sessionKey(id), strconv.FormatUint(uint64(userID), 10), ttl).Err()
}

func (r RedisSessions) Get(ctx context.Context, id string) (uint, error) {
	val, err := global.RedisClient.Get(ctx, sessionKey(id)).Result()
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseUint(val, 10, 64)
	if err != nil {
		return 0, err
	}
	return uint(n), nil
}

func (r RedisSessions) Delete(ctx context.Context, id string) error {
	return global.RedisClient.Del(ctx, sessionKey(id)).Err()
}
