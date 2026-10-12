package crypto

import "testing"

func TestRoundTrip(t *testing.T) {
	blob, err := Encrypt("jwt-secret", `{"password":"p@ss"}`)
	if err != nil {
		t.Fatal(err)
	}
	if blob == `{"password":"p@ss"}` {
		t.Fatal("ciphertext must not equal plaintext")
	}
	got, err := Decrypt("jwt-secret", blob)
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"password":"p@ss"}` {
		t.Fatalf("got %s", got)
	}
}

func TestDecryptRejectsWrongKey(t *testing.T) {
	blob, err := Encrypt("jwt-secret", "token-value")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt("other", blob); err == nil {
		t.Fatal("expected error")
	}
}
