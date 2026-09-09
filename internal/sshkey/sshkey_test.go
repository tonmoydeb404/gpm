package sshkey

import (
	"crypto/ed25519"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/tonmoydeb404/gpm/internal/perm"
)

func TestGenerateRoundtrip(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519_test")

	pub, err := Generate(keyPath, "gpm:test jane@example.com")
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if !strings.HasPrefix(pub, "ssh-ed25519 ") {
		t.Errorf("public key = %q, want ssh-ed25519 prefix", pub)
	}
	if !strings.HasSuffix(pub, "gpm:test jane@example.com\n") {
		t.Errorf("public key comment missing: %q", pub)
	}

	// The private key must parse back as an ED25519 key in OpenSSH format.
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "OPENSSH PRIVATE KEY" {
		t.Fatalf("private key is not in OpenSSH format")
	}
	parsed, err := ssh.ParseRawPrivateKey(raw)
	if err != nil {
		t.Fatalf("ParseRawPrivateKey() error = %v", err)
	}
	if _, ok := parsed.(*ed25519.PrivateKey); !ok {
		t.Fatalf("parsed key type = %T, want *ed25519.PrivateKey", parsed)
	}

	// The public key file must parse and match the returned line.
	pubData, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	if string(pubData) != pub {
		t.Errorf(".pub file = %q, want %q", pubData, pub)
	}
	if _, _, _, _, err := ssh.ParseAuthorizedKey(pubData); err != nil {
		t.Fatalf("ParseAuthorizedKey() error = %v", err)
	}
}

func TestGeneratePermissions(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519_test")
	if _, err := Generate(keyPath, ""); err != nil {
		t.Fatal(err)
	}
	ok, cur := perm.Private(keyPath)
	if !ok {
		t.Errorf("private key perms = %s, want private", cur)
	}
	ok, cur = perm.Standard(keyPath + ".pub")
	if !ok {
		t.Errorf("public key perms = %s, want standard", cur)
	}
}

func TestGenerateRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519_test")
	if _, err := Generate(keyPath, "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(keyPath, "second"); err == nil {
		t.Fatal("expected error when overwriting an existing key")
	}
}

func TestFingerprint(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519_test")
	if _, err := Generate(keyPath, "fp"); err != nil {
		t.Fatal(err)
	}
	fp, err := Fingerprint(keyPath + ".pub")
	if err != nil {
		t.Fatalf("Fingerprint() error = %v", err)
	}
	if !strings.HasPrefix(fp, "SHA256:") {
		t.Errorf("fingerprint = %q, want SHA256 prefix", fp)
	}
}

func TestRemove(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519_test")
	if _, err := Generate(keyPath, "rm"); err != nil {
		t.Fatal(err)
	}
	if err := Remove(keyPath); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if Exists(keyPath) || Exists(keyPath+".pub") {
		t.Fatal("key files still exist after Remove")
	}
	// Idempotent.
	if err := Remove(keyPath); err != nil {
		t.Fatalf("Remove() second call error = %v", err)
	}
}
