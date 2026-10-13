package bootstrap

import (
	"bak/global"
	"bak/internal/store"
	"bak/utils/cache"
	"bak/utils/conf"
	"bak/utils/database"
)

func Init() {
	//配置文件
	conf.InitConfig()

	//redis
	global.RedisClient = cache.InitRedis()
	//mysql
	global.DataBase = database.InitMysql()
	if err := store.AutoMigrate(global.DataBase); err != nil {
		panic(err)
	}
}
