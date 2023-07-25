package cache

import (
	"BaiduYunPanBak/global"
	"context"
	"fmt"
	"time"
)

func Has(key string) bool {
	ctx := context.Background()
	result, err := global.RedisClient.Exists(ctx, key).Result()

	if nil != err {
		fmt.Println(err.Error())
		return false
	}

	return result > 0
}

func Get(key string) string {
	ctx := context.Background()
	result, err := global.RedisClient.Get(ctx, key).Result()

	if nil != err {
		fmt.Println(err.Error())
		return ""
	}

	return result
}

func Set(key string, value string, ttl int) {
	redisExpire := time.Duration(ttl)

	expireTime := redisExpire * time.Second

	ctx := context.Background()
	err := global.RedisClient.Set(ctx, key, value, expireTime).Err()

	if nil != err {
		fmt.Println(err.Error())
	}
}

func SetNx(key string, value string, limit int64) bool {
	ctx := context.Background()

	limitTime := time.Duration(limit) * time.Second

	set, err := global.RedisClient.SetNX(ctx, key, value, limitTime).Result()

	if nil != err {
		fmt.Println(err.Error())
		return false
	}
	return set
}

func Del(key string) bool {
	ctx := context.Background()

	err := global.RedisClient.Del(ctx, key).Err()

	if nil != err {
		fmt.Println(err.Error())
		return false
	}

	return true
}

func Keys(key string) []string {
	ctx := context.Background()

	keys, err := global.RedisClient.Keys(ctx, key).Result()

	if err != nil {
		fmt.Println(err.Error())
	}

	return keys
}
