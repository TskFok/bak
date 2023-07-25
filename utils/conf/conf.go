package conf

import (
	"BaiduYunPanBak/global"
	"bytes"
	_ "embed"
	"github.com/spf13/viper"
)

//go:embed conf.yaml
var conf []byte

func InitConfig() {
	viper.SetConfigType("yaml")

	err := viper.ReadConfig(bytes.NewReader(conf))

	if nil != err {
		panic(err)
	}

	global.RedisUser = viper.Get("redis.user").(string)
	global.RedisPassword = viper.Get("redis.password").(string)
	global.RedisHost = viper.Get("redis.host").(string)
	global.MysqlDsn = viper.Get("mysql.dsn").(string)
	global.MysqlPrefix = viper.Get("mysql.prefix").(string)
	global.BaiduYunClientId = viper.Get("baidu_yun.client_id").(string)
	global.BaiduYunClientSecret = viper.Get("baidu_yun.client_secret").(string)
}
