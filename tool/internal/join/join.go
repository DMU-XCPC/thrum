// Package join turns this machine into another site of a workspace that already
// exists: it takes the shared line, then fills in what a repository cannot carry.
package join

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
	"github.com/multiagentworkspace/thrum/tool/internal/line"
	"github.com/multiagentworkspace/thrum/tool/internal/records"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// Token is what an invitation carries: the workspace, the new site, the inviting
// site's address and key, and the one-time nonce. It names no repository and no URL
// the joiner has to understand.
type Token struct {
	Workspace string `json:"workspace"`
	Site      string `json:"site"`
	Peer      string `json:"peer"`
	Reach     string `json:"reach"`
	PeerKey   string `json:"peer_key"`
	At        string `json:"at,omitempty"`
	Nonce     string `json:"nonce"`
}

// Encode renders a token as one copy-pasteable string.
func Encode(t Token) (string, error) {
	data, err := json.Marshal(t)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

// Decode reads a token.
func Decode(s string) (Token, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return Token{}, fmt.Errorf("that is not an invitation token: %w", err)
	}
	var t Token
	if err := json.Unmarshal(raw, &t); err != nil {
		return Token{}, fmt.Errorf("that is not an invitation token: %w", err)
	}
	// The address may be absent: the joiner then asks the LAN. Identity, peer and
	// nonce are not optional.
	if t.Site == "" || t.Peer == "" || t.PeerKey == "" || t.Nonce == "" {
		return Token{}, fmt.Errorf("the invitation is incomplete")
	}
	return t, nil
}

// Join makes the workspace at root another site of the token's workspace. It takes
// the line, the ledger and the store through the inviting site's daemon, registers
// its key there, and leaves no repository with a thrum remote.
func Join(root string, t Token) error {
	if t.Reach == "" {
		return fmt.Errorf("the invitation names no address and the LAN did not answer")
	}
	existed := false
	if _, err := os.Stat(root); err == nil {
		existed = true
	}
	if entries, err := os.ReadDir(root); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s already exists and is not empty", root)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	done := false
	defer func() {
		if done {
			return
		}
		if !existed {
			_ = os.RemoveAll(root)
			return
		}
		entries, _ := os.ReadDir(root)
		for _, e := range entries {
			_ = os.RemoveAll(filepath.Join(root, e.Name()))
		}
	}()

	encoded, err := Encode(t)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if strings.ContainsAny(exe, " \t\n") {
		return fmt.Errorf("the thrum binary path contains a space; the join transport cannot carry it")
	}
	url := func(repo, service string) string {
		return "ext::" + exe + " git-proxy --join-token " + encoded +
			" --repo " + repo + " --service " + service
	}

	// The line comes first: it is the history everything else is understood against.
	if err := gitx.OK("", "-c", "protocol.ext.allow=always", "clone", url("line", "upload-pack"), root); err != nil {
		return err
	}
	l := layout.New(root)
	if _, err := gitx.Run(l.Root, "rev-parse", "--verify", "HEAD"); err != nil {
		if err := gitx.OK(l.Root, "checkout", "-B", "main", "origin/main"); err != nil {
			return err
		}
	}
	_ = gitx.OK(l.Root, "remote", "remove", "origin")

	// Refuse before writing anything: a token from another workspace, a site the
	// roster does not declare, and a used invitation all leave nothing behind.
	if t.Workspace != "" {
		got, err := WorkspaceID(l.Root)
		if err != nil {
			return err
		}
		if got != t.Workspace {
			return fmt.Errorf("that line is a different workspace")
		}
	}
	var site records.Site
	ok, err := records.Read(l.Site(t.Site), &site)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("the roster does not declare %q: invite it first", t.Site)
	}
	if site.Invite == "" || site.Invite != InviteHash(t.Nonce) {
		return fmt.Errorf("that invitation has been used, or it names another site")
	}

	if err := workspace.WriteSkeleton(l); err != nil {
		return err
	}
	ignore, err := workspace.IgnoreList()
	if err != nil {
		return err
	}
	if err := os.WriteFile(l.GitIgnore(), ignore, 0o644); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.InfoExclude()), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(l.InfoExclude(), ignore, 0o644); err != nil {
		return err
	}
	for _, d := range l.EmptyDirs() {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}

	// The ledger.
	if err := gitx.OK("", "-c", "protocol.ext.allow=always", "clone", url("ledger", "upload-pack"), l.Ledger()); err != nil {
		return err
	}
	if _, err := gitx.Run(l.Ledger(), "rev-parse", "--verify", "HEAD"); err != nil {
		if err := gitx.OK(l.Ledger(), "checkout", "-B", "main", "origin/main"); err != nil {
			return err
		}
	}
	_ = gitx.OK(l.Ledger(), "remote", "remove", "origin")
	for _, dir := range []string{l.Root, l.Ledger()} {
		if err := gitx.OK(dir, "config", "receive.denyCurrentBranch", "updateInstead"); err != nil {
			return err
		}
	}

	// The store, bare.
	if err := gitx.OK("", "-c", "protocol.ext.allow=always", "clone", "--bare", url("store", "upload-pack"), l.Store()); err != nil {
		return err
	}
	_ = gitx.OK(l.Store(), "remote", "remove", "origin")

	// The code repositories are the project's own: the declaration is in the line,
	// the content is not carried by thrum.

	if err := os.MkdirAll(l.ConfigDir(), 0o755); err != nil {
		return err
	}
	data, err := format.MarshalYAML(struct {
		Endpoint string `yaml:"endpoint"`
	}{Endpoint: t.Reach})
	if err != nil {
		return err
	}
	if err := format.WriteFileAtomic(filepath.Join(l.ConfigDir(), t.Peer+".yaml"), data); err != nil {
		return err
	}

	if err := os.WriteFile(l.SiteFile(), []byte(t.Site+"\n"), 0o644); err != nil {
		return err
	}
	seed, pub, err := workspace.GenerateSiteKey()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(l.SecretsDir(), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(l.PrivateKey(), []byte(base64.StdEncoding.EncodeToString(seed)+"\n"), 0o600); err != nil {
		return err
	}

	site.Key = pub
	site.Invite = ""
	if err := records.Write(l.Site(t.Site), site); err != nil {
		return err
	}
	if err := line.Commit(l.Root, []string{l.Site(t.Site)},
		fmt.Sprintf("site %s: key registered", t.Site)); err != nil {
		return err
	}
	if err := gitx.OK(l.Root, "-c", "protocol.ext.allow=always", "push",
		url("line", "receive-pack"), "main"); err != nil {
		return fmt.Errorf("the key is recorded here, but it could not be published: %w", err)
	}
	done = true
	return nil
}

// NewNonce is the one-time secret an invitation carries.
func NewNonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// InviteHash is what the roster keeps, so a leaked roster does not leak a token.
func InviteHash(nonce string) string {
	sum := sha256.Sum256([]byte(nonce))
	return hex.EncodeToString(sum[:])
}

// WorkspaceID is the line's first commit: the durable name of the workspace.
func WorkspaceID(dir string) (string, error) {
	out, err := gitx.Run(dir, "rev-list", "--max-parents=0", "HEAD")
	if err != nil {
		return "", err
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", fmt.Errorf("the line has no first commit")
	}
	return fields[0], nil
}
