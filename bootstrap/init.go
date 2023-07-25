package bootstrap

import (
	"BaiduYunPanBak/global"
	"BaiduYunPanBak/utils/cache"
	"BaiduYunPanBak/utils/conf"
	"BaiduYunPanBak/utils/database"
)

func Init() {
	//配置文件
	conf.InitConfig()

	//redis
	global.RedisClient = cache.InitRedis()
	//mysql
	global.DataBase = database.InitMysql()
}
