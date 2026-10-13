package cmd

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"
)

func TestValidateServeAddrLoopbackOnlyByDefault(t *testing.T) {
	t.Setenv("BAIDUBAK_ALLOW_UNSPECIFIED_ADDR", "")
	if err := validateServeAddr("127.0.0.1:8080"); err != nil {
		t.Fatal(err)
	}
	if err := validateServeAddr("0.0.0.0:8080"); err == nil {
		t.Fatal("expected reject")
	}
}

func TestValidateServeAddrAllowsUnspecifiedInContainer(t *testing.T) {
	t.Setenv("BAIDUBAK_ALLOW_UNSPECIFIED_ADDR", "1")
	if err := validateServeAddr("0.0.0.0:8080"); err != nil {
		t.Fatal(err)
	}
	if err := validateServeAddr("192.168.1.10:8080"); err == nil {
		t.Fatal("expected reject")
	}
}

func TestParseArgsDefaults(t *testing.T) {
	opt, err := parseArgs(nil, ioDiscard(t))
	if err != nil {
		t.Fatal(err)
	}
	if opt.addr != "127.0.0.1:8080" || opt.dev {
		t.Fatalf("%+v", opt)
	}
}

func TestParseArgsFlags(t *testing.T) {
	opt, err := parseArgs([]string{"--addr", "127.0.0.1:9090", "--dev"}, ioDiscard(t))
	if err != nil {
		t.Fatal(err)
	}
	if opt.addr != "127.0.0.1:9090" || !opt.dev {
		t.Fatalf("%+v", opt)
	}
}

func TestParseArgsRejectsSubcommands(t *testing.T) {
	cases := [][]string{
		{"serve"},
		{"upload", "/tmp/a.txt"},
		{"flashToken", "code"},
		{"reflashToken"},
	}
	for _, args := range cases {
		var buf bytes.Buffer
		_, err := parseArgs(args, &buf)
		if err == nil {
			t.Fatalf("accepted %v", args)
		}
		if !strings.Contains(buf.String(), "不接受命令行子命令") {
			t.Fatalf("args %v message %q", args, buf.String())
		}
	}
}

func TestParseArgsHelp(t *testing.T) {
	var buf bytes.Buffer
	_, err := parseArgs([]string{"-h"}, &buf)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(buf.String(), "-addr") || !strings.Contains(buf.String(), "-dev") {
		t.Fatalf("usage %q", buf.String())
	}
}

func ioDiscard(t *testing.T) *bytes.Buffer {
	t.Helper()
	return &bytes.Buffer{}
}
