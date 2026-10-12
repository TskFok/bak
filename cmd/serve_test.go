package cmd

import "testing"

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
