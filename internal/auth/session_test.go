package auth

import "testing"

func TestNewSessionID(t *testing.T) {
	a, err := NewSessionID()
	if err != nil || len(a) < 32 {
		t.Fatalf("id %q err %v", a, err)
	}
	b, _ := NewSessionID()
	if a == b {
		t.Fatal("session ids collided")
	}
}
