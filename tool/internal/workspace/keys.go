package workspace

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

// GenerateSiteKey makes this site's key pair, and returns the seed to store and the
// public key in the form the roster carries.
func GenerateSiteKey() (seed []byte, public string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil, "", err
	}
	return priv.Seed(), strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))), nil
}

// ParseSiteKey reads a roster's public key back into an ed25519 key.
func ParseSiteKey(line string) (ed25519.PublicKey, error) {
	pk, _, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.TrimSpace(line)))
	if err != nil {
		return nil, fmt.Errorf("site key: %w", err)
	}
	cpk, ok := pk.(ssh.CryptoPublicKey)
	if !ok {
		return nil, fmt.Errorf("site key: not a crypto public key")
	}
	ed, ok := cpk.CryptoPublicKey().(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("site key: not an ed25519 key")
	}
	return ed, nil
}
