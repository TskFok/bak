package global

import (
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

var RedisUser string
var RedisPassword string
var RedisHost string
var MysqlDsn string
var MysqlPrefix string
var RedisClient *redis.Client
var DataBase *gorm.DB
var BaiduYunClientId string
var BaiduYunClientSecret string
