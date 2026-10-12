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

func TestDockerComposeExample(t *testing.T) {
	data, err := os.ReadFile("docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	mustContain(t, text, "image: baidubak:linux-amd64")
	mustContain(t, text, "platform: linux/amd64")
	mustContain(t, text, "network_mode: host")
	mustContain(t, text, "MYSQL_DATABASE: yy")
	mustContain(t, text, "127.0.0.1:3306:3306")
	mustContain(t, text, "127.0.0.1:6379:6379")
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
