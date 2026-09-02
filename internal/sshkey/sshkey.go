// Package sshkey generates and manages ED25519 SSH key pairs owned by
// gpm profiles. Private keys never leave the machine and are written
// with 0600 permissions in OpenSSH format.
package sshkey

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/tonmoydeb/gpm/internal/config"
)

// Generate creates a new ED25519 key pair at privatePath. The private
// key is written in OpenSSH format with 0600 permissions; the public
// key next to it (privatePath+".pub") with 0644. It refuses to
// overwrite an existing private key.
//
// Returns the public key in authorized_keys format.
func Generate(privatePath, comment string) (string, error) {
	if _, err := os.Stat(privatePath); err == nil {
		return "", fmt.Errorf("refusing to overwrite existing key %s (use --force to regenerate)", privatePath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("stat %s: %w", privatePath, err)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate ed25519 key: %w", err)
	}

	block, err := ssh.MarshalPrivateKey(priv, comment)
	if err != nil {
		return "", fmt.Errorf("marshal private key: %w", err)
	}
	privatePEM := pem.EncodeToMemory(block)

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", fmt.Errorf("marshal public key: %w", err)
	}
	authorizedKey := authorizedKeyLine(sshPub, comment)

	if err := config.AtomicWrite(privatePath, privatePEM, 0o600); err != nil {
		return "", err
	}
	if err := os.Chmod(privatePath, 0o600); err != nil {
		return "", fmt.Errorf("chmod %s: %w", privatePath, err)
	}
	pubPath := PubPath(privatePath)
	if err := config.AtomicWrite(pubPath, []byte(authorizedKey), 0o644); err != nil {
		return "", err
	}
	if err := os.Chmod(pubPath, 0o644); err != nil {
		return "", fmt.Errorf("chmod %s: %w", pubPath, err)
	}
	return string(authorizedKey), nil
}

// Remove deletes a key pair (private key and .pub). Missing files are
// ignored so removal is idempotent.
func Remove(privatePath string) error {
	var errs []error
	for _, p := range []string{privatePath, PubPath(privatePath)} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove %s: %w", p, err))
		}
	}
	return errors.Join(errs...)
}

// Exists reports whether the private key file is present.
func Exists(privatePath string) bool {
	_, err := os.Stat(privatePath)
	return err == nil
}

// PublicKey loads the public key of a pair in authorized_keys format.
func PublicKey(privatePath string) (string, error) {
	data, err := os.ReadFile(PubPath(privatePath))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", PubPath(privatePath), err)
	}
	return string(data), nil
}

// Fingerprint returns the SHA256 fingerprint of a public key file.
func Fingerprint(pubPath string) (string, error) {
	data, err := os.ReadFile(pubPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", pubPath, err)
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey(data)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", pubPath, err)
	}
	return ssh.FingerprintSHA256(pub), nil
}

// PubPath returns the path of the public key for a private key path.
func PubPath(privatePath string) string {
	return strings.TrimSuffix(privatePath, ".pub") + ".pub"
}

// authorizedKeyLine renders a public key in authorized_keys format
// including the trailing comment. (x/crypto's MarshalAuthorizedKey
// omits the comment.)
func authorizedKeyLine(key ssh.PublicKey, comment string) string {
	line := key.Type() + " " + base64.StdEncoding.EncodeToString(key.Marshal())
	if comment != "" {
		line += " " + comment
	}
	return line + "\n"
}
