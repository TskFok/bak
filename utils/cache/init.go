package cache

import (
	"BaiduYunPanBak/global"
	"context"
	"github.com/redis/go-redis/v9"
)

func InitRedis() *redis.Client {
	client := redis.NewClient(&redis.Options{
		Addr:     global.RedisHost,
		Password: global.RedisPassword,
		DB:       0,
		Username: global.RedisUser,
	})

	ctx := context.Background()

	cmd := client.Ping(ctx)

	if nil != cmd.Err() {
		panic(cmd.Err())
	}

	return client
}
