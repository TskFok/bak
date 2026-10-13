package conf

import (
	"bak/global"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	os.Setenv("BAIDUBAK_JWT_SECRET", "test-jwt-secret")
	os.Exit(m.Run())
}

func TestInitConfigUsesEmbeddedDSN(t *testing.T) {
	t.Setenv("BAIDUBAK_MYSQL_DSN", "")
	t.Setenv("BAIDUBAK_REDIS_HOST", "")
	t.Setenv("BAIDUBAK_CONFIG", "")
	withNoDefaultMount(t)

	InitConfig()

	const want = "root:@tcp(127.0.0.1:3306)/yy?charset=utf8mb4&parseTime=True&loc=Local"
	if global.MysqlDsn != want {
		t.Fatalf("dsn = %q", global.MysqlDsn)
	}
}

func TestMountedConfigOverridesDSN(t *testing.T) {
	t.Setenv("BAIDUBAK_MYSQL_DSN", "")
	t.Setenv("BAIDUBAK_REDIS_HOST", "")
	t.Setenv("BAIDUBAK_CONFIG", "")
	path := writeConfig(t, "mysql:\n  dsn: user:secret@tcp(10.0.0.8:3306)/yy?charset=utf8mb4&parseTime=True&loc=Local\n")
	setDefaultMount(t, path)

	InitConfig()

	const want = "user:secret@tcp(10.0.0.8:3306)/yy?charset=utf8mb4&parseTime=True&loc=Local"
	if global.MysqlDsn != want {
		t.Fatalf("dsn = %q", global.MysqlDsn)
	}
	if global.RedisHost != "127.0.0.1:6379" {
		t.Fatalf("redis host = %q", global.RedisHost)
	}
	if global.MysqlPrefix != "" {
		t.Fatalf("prefix = %q", global.MysqlPrefix)
	}
}

func TestEnvDSNOverridesMountedConfig(t *testing.T) {
	t.Setenv("BAIDUBAK_REDIS_HOST", "")
	t.Setenv("BAIDUBAK_CONFIG", "")
	path := writeConfig(t, "mysql:\n  dsn: from-file@tcp(10.0.0.8:3306)/yy\n")
	setDefaultMount(t, path)
	t.Setenv("BAIDUBAK_MYSQL_DSN", "from-env@tcp(10.1.1.1:3306)/yy")

	InitConfig()

	if global.MysqlDsn != "from-env@tcp(10.1.1.1:3306)/yy" {
		t.Fatalf("dsn = %q", global.MysqlDsn)
	}
}

func TestEnvRedisHostOverridesMountedConfig(t *testing.T) {
	t.Setenv("BAIDUBAK_MYSQL_DSN", "")
	t.Setenv("BAIDUBAK_CONFIG", "")
	path := writeConfig(t, "redis:\n  host: 10.0.0.9:6379\n  password: from-file\n")
	setDefaultMount(t, path)
	t.Setenv("BAIDUBAK_REDIS_HOST", "10.1.1.2:6379")
	t.Setenv("BAIDUBAK_REDIS_USER", "")
	t.Setenv("BAIDUBAK_REDIS_PASSWORD", "")

	InitConfig()

	if global.RedisHost != "10.1.1.2:6379" {
		t.Fatalf("redis host = %q", global.RedisHost)
	}
	if global.RedisPassword != "from-file" {
		t.Fatalf("redis password = %q", global.RedisPassword)
	}
}

func TestEnvRedisUserPasswordOverrideMountedConfig(t *testing.T) {
	t.Setenv("BAIDUBAK_MYSQL_DSN", "")
	t.Setenv("BAIDUBAK_CONFIG", "")
	path := writeConfig(t, "redis:\n  host: 10.0.0.9:6379\n  user: file-user\n  password: from-file\n")
	setDefaultMount(t, path)
	t.Setenv("BAIDUBAK_REDIS_HOST", "10.1.1.2:6379")
	t.Setenv("BAIDUBAK_REDIS_USER", "env-user")
	t.Setenv("BAIDUBAK_REDIS_PASSWORD", "env-secret")

	InitConfig()

	if global.RedisHost != "10.1.1.2:6379" {
		t.Fatalf("redis host = %q", global.RedisHost)
	}
	if global.RedisUser != "env-user" {
		t.Fatalf("redis user = %q", global.RedisUser)
	}
	if global.RedisPassword != "env-secret" {
		t.Fatalf("redis password = %q", global.RedisPassword)
	}
}

func TestBlankEnvRedisUserPasswordDoNotOverride(t *testing.T) {
	t.Setenv("BAIDUBAK_MYSQL_DSN", "")
	t.Setenv("BAIDUBAK_REDIS_HOST", "")
	t.Setenv("BAIDUBAK_REDIS_USER", "   ")
	t.Setenv("BAIDUBAK_REDIS_PASSWORD", "   ")
	t.Setenv("BAIDUBAK_CONFIG", "")
	path := writeConfig(t, "redis:\n  user: file-user\n  password: from-file\n")
	setDefaultMount(t, path)

	InitConfig()

	if global.RedisUser != "file-user" {
		t.Fatalf("redis user = %q", global.RedisUser)
	}
	if global.RedisPassword != "from-file" {
		t.Fatalf("redis password = %q", global.RedisPassword)
	}
}

func TestBlankEnvRedisHostDoesNotOverride(t *testing.T) {
	t.Setenv("BAIDUBAK_REDIS_HOST", "   ")
	t.Setenv("BAIDUBAK_MYSQL_DSN", "")
	t.Setenv("BAIDUBAK_CONFIG", "")
	withNoDefaultMount(t)

	InitConfig()

	if global.RedisHost != "127.0.0.1:6379" {
		t.Fatalf("redis host = %q", global.RedisHost)
	}
}

func TestBlankEnvDSNDoesNotOverride(t *testing.T) {
	t.Setenv("BAIDUBAK_MYSQL_DSN", "   ")
	t.Setenv("BAIDUBAK_CONFIG", "")
	withNoDefaultMount(t)

	InitConfig()

	if !strings.Contains(global.MysqlDsn, "127.0.0.1:3306") {
		t.Fatalf("dsn = %q", global.MysqlDsn)
	}
}

func TestExplicitConfigPathOverridesDSN(t *testing.T) {
	t.Setenv("BAIDUBAK_MYSQL_DSN", "")
	t.Setenv("BAIDUBAK_REDIS_HOST", "")
	path := writeConfig(t, "mysql:\n  dsn: mounted@tcp(db.internal:3306)/backup\n")
	t.Setenv("BAIDUBAK_CONFIG", path)
	setDefaultMount(t, filepath.Join(t.TempDir(), "missing.yaml"))

	InitConfig()

	if global.MysqlDsn != "mounted@tcp(db.internal:3306)/backup" {
		t.Fatalf("dsn = %q", global.MysqlDsn)
	}
}

func TestExplicitConfigMissingUsesEmbedded(t *testing.T) {
	t.Setenv("BAIDUBAK_MYSQL_DSN", "")
	t.Setenv("BAIDUBAK_REDIS_HOST", "")
	t.Setenv("BAIDUBAK_CONFIG", filepath.Join(t.TempDir(), ".env"))
	withNoDefaultMount(t)

	InitConfig()

	if !strings.Contains(global.MysqlDsn, "127.0.0.1:3306") {
		t.Fatalf("dsn = %q", global.MysqlDsn)
	}
}

func TestDotEnvFileOverridesDSNAndRedis(t *testing.T) {
	t.Setenv("BAIDUBAK_MYSQL_DSN", "")
	t.Setenv("BAIDUBAK_REDIS_HOST", "")
	t.Setenv("BAIDUBAK_REDIS_USER", "")
	t.Setenv("BAIDUBAK_REDIS_PASSWORD", "")
	path := writeRaw(t, ".env", ""+
		"BAIDUBAK_MYSQL_DSN=from-dotenv@tcp(10.0.0.8:3306)/yy\n"+
		"BAIDUBAK_REDIS_HOST=10.0.0.9:6379\n"+
		"BAIDUBAK_REDIS_USER=dotenv-user\n"+
		"BAIDUBAK_REDIS_PASSWORD=dotenv-secret\n")
	t.Setenv("BAIDUBAK_CONFIG", path)
	withNoDefaultMount(t)

	InitConfig()

	if global.MysqlDsn != "from-dotenv@tcp(10.0.0.8:3306)/yy" {
		t.Fatalf("dsn = %q", global.MysqlDsn)
	}
	if global.RedisHost != "10.0.0.9:6379" {
		t.Fatalf("redis host = %q", global.RedisHost)
	}
	if global.RedisUser != "dotenv-user" || global.RedisPassword != "dotenv-secret" {
		t.Fatalf("redis auth = %q %q", global.RedisUser, global.RedisPassword)
	}
}

func TestEnvJWTSecretOverridesEmbedded(t *testing.T) {
	t.Setenv("BAIDUBAK_JWT_SECRET", "from-env-jwt")
	t.Setenv("BAIDUBAK_CONFIG", "")
	withNoDefaultMount(t)

	InitConfig()

	if global.JwtSecret != "from-env-jwt" {
		t.Fatalf("jwt secret = %q", global.JwtSecret)
	}
}

func TestDotEnvJWTSecretUsedWhenEnvEmpty(t *testing.T) {
	t.Setenv("BAIDUBAK_JWT_SECRET", "")
	path := writeRaw(t, ".env", "BAIDUBAK_JWT_SECRET=from-dotenv-jwt\n")
	t.Setenv("BAIDUBAK_CONFIG", path)
	withNoDefaultMount(t)

	InitConfig()

	if global.JwtSecret != "from-dotenv-jwt" {
		t.Fatalf("jwt secret = %q", global.JwtSecret)
	}
}

func TestMissingJWTSecretPanics(t *testing.T) {
	t.Setenv("BAIDUBAK_JWT_SECRET", "")
	t.Setenv("BAIDUBAK_CONFIG", "")
	withNoDefaultMount(t)

	defer func() {
		if recover() == nil {
			t.Fatal("empty jwt secret did not panic")
		}
	}()
	InitConfig()
}

func TestEmbeddedConfigOmitsJWTSecret(t *testing.T) {
	data, err := os.ReadFile("conf.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == `secret: ""` || strings.HasPrefix(strings.TrimSpace(line), "secret: \"\"") {
			return
		}
	}
	t.Fatal("committed conf.yaml still has a jwt secret value")
}

func TestEnvOverridesDotEnvFile(t *testing.T) {
	path := writeRaw(t, ".env", "BAIDUBAK_MYSQL_DSN=from-dotenv@tcp(10.0.0.8:3306)/yy\n")
	t.Setenv("BAIDUBAK_CONFIG", path)
	t.Setenv("BAIDUBAK_MYSQL_DSN", "from-env@tcp(10.1.1.1:3306)/yy")
	withNoDefaultMount(t)

	InitConfig()

	if global.MysqlDsn != "from-env@tcp(10.1.1.1:3306)/yy" {
		t.Fatalf("dsn = %q", global.MysqlDsn)
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	return writeRaw(t, "conf.yaml", body)
}

func writeRaw(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func setDefaultMount(t *testing.T, path string) {
	t.Helper()
	prev := defaultMountedConfigPath
	defaultMountedConfigPath = path
	t.Cleanup(func() { defaultMountedConfigPath = prev })
}

func withNoDefaultMount(t *testing.T) {
	t.Helper()
	setDefaultMount(t, filepath.Join(t.TempDir(), "absent.yaml"))
}
