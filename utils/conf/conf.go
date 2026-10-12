package conf

import (
	"BaiduYunPanBak/global"
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

//go:embed conf.yaml
var conf []byte

// defaultMountedConfigPath 是容器内挂载配置的默认位置。
// 文件存在时合并进内置配置；BAIDUBAK_CONFIG 指向的文件优先，且必须存在。
var defaultMountedConfigPath = "/app/conf.yaml"

func InitConfig() {
	viper.Reset()
	viper.SetConfigType("yaml")
	if err := viper.ReadConfig(bytes.NewReader(conf)); err != nil {
		panic(err)
	}
	if err := mergeOverlayConfig(); err != nil {
		panic(err)
	}
	setFromEnv("mysql.dsn", "BAIDUBAK_MYSQL_DSN")
	setFromEnv("redis.host", "BAIDUBAK_REDIS_HOST")
	setFromEnv("redis.user", "BAIDUBAK_REDIS_USER")
	setFromEnv("redis.password", "BAIDUBAK_REDIS_PASSWORD")

	global.RedisUser = viper.Get("redis.user").(string)
	global.RedisPassword = viper.Get("redis.password").(string)
	global.RedisHost = viper.Get("redis.host").(string)
	global.MysqlDsn = viper.Get("mysql.dsn").(string)
	global.MysqlPrefix = viper.Get("mysql.prefix").(string)
	global.BaiduYunClientId = viper.Get("baidu_yun.client_id").(string)
	global.BaiduYunClientSecret = viper.Get("baidu_yun.client_secret").(string)
	global.JwtSecret = viper.GetString("jwt.secret")
	global.JwtExpire = viper.GetInt("jwt.expire")
	if global.JwtExpire <= 0 {
		global.JwtExpire = 3
	}
}

func setFromEnv(key, env string) {
	if value := strings.TrimSpace(os.Getenv(env)); value != "" {
		viper.Set(key, value)
	}
}

func mergeOverlayConfig() error {
	path := overlayConfigPath()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取配置 %s: %w", path, err)
	}
	if err := viper.MergeConfig(bytes.NewReader(data)); err != nil {
		return fmt.Errorf("合并配置 %s: %w", path, err)
	}
	return nil
}

func overlayConfigPath() string {
	if path := strings.TrimSpace(os.Getenv("BAIDUBAK_CONFIG")); path != "" {
		return path
	}
	if _, err := os.Stat(defaultMountedConfigPath); err == nil {
		return defaultMountedConfigPath
	}
	return ""
}
