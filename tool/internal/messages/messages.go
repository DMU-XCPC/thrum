// Package messages owns a mailbox: the transient files one actor leaves for another.
package messages

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
	"github.com/multiagentworkspace/thrum/tool/internal/records"
)

// NewID is opaque and unique: a message id carries nothing, because it is for a
// machine, and the subject is for a person.
func NewID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// Send writes a message into its recipient's mailbox. The attachments are written
// first and the record that names them last, so a reader never sees a message whose
// bytes have not arrived.
func Send(l layout.Layout, msg records.Message, body []byte, files map[string][]byte) error {
	if msg.ID == "" || msg.To == "" || msg.From == "" {
		return fmt.Errorf("a message needs an id, a sender and a recipient")
	}
	box := l.Mailbox(msg.To)
	if err := os.MkdirAll(box, 0o700); err != nil {
		return err
	}
	path := filepath.Join(box, msg.ID+".md")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("message %s is already in %s's mailbox", msg.ID, msg.To)
	}

	if len(files) > 0 {
		dir := filepath.Join(box, msg.ID)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		names := make([]string, 0, len(files))
		for name := range files {
			name = filepath.Base(name)
			if name == "" || name == "." || name == string(filepath.Separator) {
				return fmt.Errorf("%q is not a file name", name)
			}
			if err := os.WriteFile(filepath.Join(dir, name), files[name], 0o600); err != nil {
				return err
			}
			names = append(names, name)
		}
		sort.Strings(names)
		msg.Attachments = nil
		for _, name := range names {
			msg.Attachments = append(msg.Attachments, records.Attachment{Name: name, Size: int64(len(files[name]))})
		}
	}

	front, err := format.MarshalYAML(msg)
	if err != nil {
		return err
	}
	return format.SaveDocument(path, front, body)
}

// List returns a mailbox, oldest first.
func List(l layout.Layout, actor string) ([]records.Message, error) {
	entries, err := os.ReadDir(l.Mailbox(actor))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []records.Message
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		msg, _, err := Read(l, actor, strings.TrimSuffix(e.Name(), ".md"))
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Sent != out[j].Sent {
			return out[i].Sent < out[j].Sent
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// Read returns one message and its body.
func Read(l layout.Layout, actor, id string) (records.Message, []byte, error) {
	path := filepath.Join(l.Mailbox(actor), id+".md")
	front, body, err := format.LoadDocument(path)
	if err != nil {
		return records.Message{}, nil, err
	}
	var msg records.Message
	if err := format.Decode(front, &msg); err != nil {
		return records.Message{}, nil, fmt.Errorf("%s: %w", path, err)
	}
	return msg, body, nil
}

// Purge removes a message and its attachments.
func Purge(l layout.Layout, actor, id string) error {
	box := l.Mailbox(actor)
	path := filepath.Join(box, id+".md")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("no message %s in %s's mailbox", id, actor)
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(box, id)); err != nil {
		return err
	}
	return nil
}

// AttachmentDir is where one message's attachments sit.
func AttachmentDir(l layout.Layout, actor, id string) string {
	return filepath.Join(l.Mailbox(actor), id)
}
