package bootstrap

import (
	"bak/global"
	"bak/internal/store"
	"bak/utils/cache"
	"bak/utils/conf"
	"bak/utils/database"
)

func Init() {
	//配置文件。缺密钥或 Redis 时仍要先把库表补齐，容器才能在依赖未就绪时留下表结构。
	conf.InitConfig()

	global.DataBase = database.InitMysql()
	if err := store.AutoMigrate(global.DataBase); err != nil {
		panic(err)
	}
	conf.RequireJWT()
	global.RedisClient = cache.InitRedis()
}
