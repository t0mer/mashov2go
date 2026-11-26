package crypto

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	k := bytes.Repeat([]byte{7}, 32)
	return k
}

func TestSealOpenRoundTrip(t *testing.T) {
	s, err := NewSealer(testKey(t))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := s.Seal([]byte("secret-password"), []byte("accounts.password:1"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("secret-password")) {
		t.Fatal("plaintext visible in sealed output")
	}
	pt, err := s.Open(sealed, []byte("accounts.password:1"))
	if err != nil || string(pt) != "secret-password" {
		t.Fatalf("Open = %q, %v", pt, err)
	}
}

func TestSealIsRandomized(t *testing.T) {
	s, _ := NewSealer(testKey(t))
	a, _ := s.Seal([]byte("x"), nil)
	b, _ := s.Seal([]byte("x"), nil)
	if bytes.Equal(a, b) {
		t.Fatal("two seals of the same value must differ (fresh nonce)")
	}
}

func TestOpenRejectsWrongAADAndTampering(t *testing.T) {
	s, _ := NewSealer(testKey(t))
	sealed, _ := s.Seal([]byte("x"), []byte("row-1"))
	if _, err := s.Open(sealed, []byte("row-2")); err == nil {
		t.Fatal("ciphertext moved to another context must not open")
	}
	sealed[len(sealed)-1] ^= 0xff
	if _, err := s.Open(sealed, []byte("row-1")); err == nil {
		t.Fatal("tampered ciphertext must not open")
	}
}

func TestOpenRejectsWrongKey(t *testing.T) {
	a, _ := NewSealer(testKey(t))
	other := bytes.Repeat([]byte{9}, 32)
	b, _ := NewSealer(other)
	sealed, _ := a.Seal([]byte("x"), nil)
	if _, err := b.Open(sealed, nil); err == nil {
		t.Fatal("wrong key must not open")
	}
}

func TestNewSealerRequires32ByteKey(t *testing.T) {
	if _, err := NewSealer([]byte("short")); err == nil {
		t.Fatal("expected error for short key")
	}
}

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadMasterKeyFromEnv(t *testing.T) {
	enc := base64.StdEncoding.EncodeToString(testKey(t))
	key, err := LoadMasterKey(env(map[string]string{EnvMasterKey: enc}), nil)
	if err != nil || !bytes.Equal(key, testKey(t)) {
		t.Fatalf("key = %v, err %v", key, err)
	}
}

func TestLoadMasterKeyFromFile(t *testing.T) {
	enc := base64.StdEncoding.EncodeToString(testKey(t))
	read := func(path string) ([]byte, error) {
		if path != "/run/secrets/key" {
			return nil, errors.New("unexpected path")
		}
		return []byte(enc + "\n"), nil
	}
	key, err := LoadMasterKey(env(map[string]string{EnvMasterKeyFile: "/run/secrets/key"}), read)
	if err != nil || !bytes.Equal(key, testKey(t)) {
		t.Fatalf("key from file: %v", err)
	}
}

func TestLoadMasterKeyMissingRefuses(t *testing.T) {
	_, err := LoadMasterKey(env(nil), nil)
	if !errors.Is(err, ErrNoMasterKey) {
		t.Fatalf("err = %v, want ErrNoMasterKey", err)
	}
}

func TestLoadMasterKeyRejectsBadLength(t *testing.T) {
	enc := base64.StdEncoding.EncodeToString([]byte("too short"))
	if _, err := LoadMasterKey(env(map[string]string{EnvMasterKey: enc}), nil); err == nil {
		t.Fatal("expected length error")
	}
}
