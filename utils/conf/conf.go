package conf

import (
	"BaiduYunPanBak/global"
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

//go:embed conf.yaml
var conf []byte

// defaultMountedConfigPath 是容器内挂载 YAML 的默认位置。
// 文件存在时合并进内置配置。BAIDUBAK_CONFIG 指向的文件优先。
// 路径不存在时跳过，继续使用内置配置和进程环境变量。
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
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取配置 %s: %w", path, err)
	}
	if isDotEnv(path) {
		mergeDotEnv(data)
		return nil
	}
	if err := viper.MergeConfig(bytes.NewReader(data)); err != nil {
		return fmt.Errorf("合并配置 %s: %w", path, err)
	}
	return nil
}

func isDotEnv(path string) bool {
	base := filepath.Base(strings.TrimSpace(path))
	return base == ".env" || strings.HasSuffix(base, ".env")
}

func mergeDotEnv(data []byte) {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		switch key {
		case "BAIDUBAK_MYSQL_DSN":
			setIfNotBlank("mysql.dsn", val)
		case "BAIDUBAK_REDIS_HOST":
			setIfNotBlank("redis.host", val)
		case "BAIDUBAK_REDIS_USER":
			setIfNotBlank("redis.user", val)
		case "BAIDUBAK_REDIS_PASSWORD":
			setIfNotBlank("redis.password", val)
		}
	}
}

func setIfNotBlank(key, value string) {
	if strings.TrimSpace(value) != "" {
		viper.Set(key, value)
	}
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
