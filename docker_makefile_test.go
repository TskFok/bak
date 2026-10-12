package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestMakeDockerBuildLinuxAmd64(t *testing.T) {
	out := dryRunMake(t, "docker-build-linux-amd64")
	mustContain(t, out, "--builder baidubak-builder")
	mustContain(t, out, "--platform linux/amd64")
	mustContain(t, out, "--load")
	mustContain(t, out, "-t baidubak:linux-amd64")
	if strings.Contains(out, "linux/arm64") {
		t.Fatalf("amd64 构建命令包含 arm64:\n%s", out)
	}
}

func TestMakeDockerBuildLinuxArm64(t *testing.T) {
	out := dryRunMake(t, "docker-build-linux-arm64")
	mustContain(t, out, "--builder baidubak-builder")
	mustContain(t, out, "--platform linux/arm64")
	mustContain(t, out, "--load")
	mustContain(t, out, "-t baidubak:linux-arm64")
	if strings.Contains(out, "linux/amd64") {
		t.Fatalf("arm64 构建命令包含 amd64:\n%s", out)
	}
}

func TestMakeDockerImageOverride(t *testing.T) {
	cmd := exec.Command("make", "-n", "IMAGE=registry.example.com/baidubak", "docker-build-linux-amd64")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make -n 失败: %v\n%s", err, out)
	}
	mustContain(t, string(out), "-t registry.example.com/baidubak:linux-amd64")
}

func TestEnvExample(t *testing.T) {
	data, err := os.ReadFile(".env.example")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	mustContain(t, text, "BAIDUBAK_PORT=8080")
	mustContain(t, text, "BAIDUBAK_MYSQL_PORT=3306")
	mustContain(t, text, "BAIDUBAK_REDIS_PORT=6379")
	mustContain(t, text, "BAIDUBAK_MYSQL_DSN=root:@tcp(host.docker.internal:3306)/yy?charset=utf8mb4&parseTime=True&loc=Local")
	mustContain(t, text, "BAIDUBAK_REDIS_HOST=host.docker.internal:6379")
	mustContain(t, text, "BAIDUBAK_REDIS_USER=")
	mustContain(t, text, "BAIDUBAK_REDIS_PASSWORD=")
}

func TestDockerComposeExample(t *testing.T) {
	data, err := os.ReadFile("docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	mustContain(t, text, "image: baidubak:linux-amd64")
	mustContain(t, text, "platform: linux/amd64")
	if strings.Contains(text, "network_mode:") {
		t.Fatalf("compose 仍使用 network_mode:\n%s", text)
	}
	mustContain(t, text, `127.0.0.1:${BAIDUBAK_PORT:-8080}:8080`)
	mustContain(t, text, `command: ["serve", "--addr", "0.0.0.0:8080"]`)
	mustContain(t, text, `BAIDUBAK_ALLOW_UNSPECIFIED_ADDR: "1"`)
	mustContain(t, text, "host.docker.internal:host-gateway")
	mustContain(t, text, "BAIDUBAK_PORT")
	mustContain(t, text, "BAIDUBAK_MYSQL_DSN")
	mustContain(t, text, "root:@tcp(host.docker.internal:3306)/yy?charset=utf8mb4&parseTime=True&loc=Local")
	mustContain(t, text, "BAIDUBAK_REDIS_HOST")
	mustContain(t, text, "host.docker.internal:6379")
	mustContain(t, text, "BAIDUBAK_REDIS_USER")
	mustContain(t, text, "BAIDUBAK_REDIS_PASSWORD")
	mustContain(t, text, "/app/conf.yaml")
	mustContain(t, text, "required: false")
	mustContain(t, text, `profiles: ["mysql"]`)
	mustContain(t, text, `profiles: ["redis"]`)
	mustContain(t, text, "MYSQL_DATABASE: yy")
	mustContain(t, text, "BAIDUBAK_MYSQL_PORT")
	mustContain(t, text, "BAIDUBAK_REDIS_PORT")
	mustContain(t, text, "127.0.0.1:${BAIDUBAK_MYSQL_PORT:-3306}:3306")
	mustContain(t, text, "127.0.0.1:${BAIDUBAK_REDIS_PORT:-6379}:6379")
}

func TestDockerfileCrossCompiles(t *testing.T) {
	data, err := os.ReadFile("Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	mustContain(t, text, "ARG TARGETOS")
	mustContain(t, text, "ARG TARGETARCH")
	mustContain(t, text, "--platform=$BUILDPLATFORM")
	mustContain(t, text, "CGO_ENABLED=0")
}

func dryRunMake(t *testing.T, target string) string {
	t.Helper()
	cmd := exec.Command("make", "-n", target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make -n %s 失败: %v\n%s", target, err, out)
	}
	return string(out)
}

func mustContain(t *testing.T, text, part string) {
	t.Helper()
	if !strings.Contains(text, part) {
		t.Fatalf("输出中缺少 %q:\n%s", part, text)
	}
}
