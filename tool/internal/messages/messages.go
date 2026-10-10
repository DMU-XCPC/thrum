// Package messages is actors reaching each other.
//
// A message is process information: it is not a record, it is never committed, it does
// not travel by git, and nothing about it is validated. It exists so that one actor can
// tell another something now — that a job is free, that the job it waited on is done,
// that the file it wanted is ready.
//
// Two properties follow from that and are enforced here rather than left to manners. It
// is delivered or refused, never queued, because a workspace that assumes delivery
// loses messages quietly. And it cannot be retracted or edited, because an agent's own
// tooling may already have acted on it: a correction is another message.
package messages

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// Attachment is a file that travelled with a message. The bytes sit beside it.
type Attachment struct {
	Name string `yaml:"name" json:"name"`
	Size int64  `yaml:"size" json:"size"`
}

// Message is one message, as it is written down.
//
// ID and Subject are separate on purpose: an identity is for a machine and a title is
// for a person, and a name that tries to be both is a name that has to be parsed.
type Message struct {
	ID          string       `yaml:"id" json:"id"`
	From        string       `yaml:"from" json:"from"`
	To          string       `yaml:"to" json:"to"`
	Site        string       `yaml:"site" json:"site"`
	Sent        string       `yaml:"sent" json:"sent"`
	ReplyTo     string       `yaml:"reply_to,omitempty" json:"reply_to,omitempty"`
	Subject     string       `yaml:"subject" json:"subject"`
	Job         string       `yaml:"job,omitempty" json:"job,omitempty"`
	Attachments []Attachment `yaml:"attachments,omitempty" json:"attachments,omitempty"`
}

// NewID makes an opaque identity for a message.
//
// It is deliberately unreadable: nothing should be tempted to parse it, and the subject
// is where a person looks. The time is not in it either, because the message already
// carries `sent` and an id that lies about when it was made is worse than one that says
// nothing.
func NewID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Randomness failing is not something to paper over with a timestamp: two
		// messages must never share an id.
		panic(fmt.Sprintf("messages: no randomness for a message id: %v", err))
	}
	return "m-" + hex.EncodeToString(b[:])
}

// Deliver writes a message into an actor's mailbox at this site, with its attachments.
//
// The mailbox is a directory, so this needs no daemon: on one machine actors reach each
// other by writing files, and only a recipient at another site needs a service.
func Deliver(l workspace.Layout, m Message, body string, files map[string][]byte) error {
	if m.ID == "" || m.Subject == "" || m.From == "" || m.To == "" {
		return fmt.Errorf("a message needs an id, a sender, a recipient and a subject")
	}
	if !format.ValidSlug(m.To) || !format.ValidSlug(m.From) {
		return fmt.Errorf("an actor name is a slug")
	}
	for _, f := range m.Attachments {
		if _, ok := files[f.Name]; !ok {
			return fmt.Errorf("%s was named as an attachment and its bytes are missing", f.Name)
		}
	}

	dir := l.Mailbox(m.To)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, m.ID+".md")
	if _, err := os.Stat(path); err == nil {
		// An id is unique, and a collision would silently overwrite somebody's words.
		return fmt.Errorf("%s is already in %s's mailbox", m.ID, m.To)
	}

	header, err := format.MarshalYAML(m)
	if err != nil {
		return err
	}
	// The subject is in the front-matter, where a list can read it without parsing the
	// body; the body is what the sender wrote.
	text := body
	if text == "" {
		text = m.Subject
	}
	if len(files) > 0 {
		filesDir := filepath.Join(dir, m.ID)
		if err := os.MkdirAll(filesDir, 0o755); err != nil {
			return err
		}
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(filesDir, filepath.Base(name)), data, 0o644); err != nil {
				return err
			}
		}
	}
	// One blank line after the front-matter, then what was written: a message reads
	// like a document because it is one.
	return format.SaveDocument(path, header, []byte("\n"+text))
}

// List returns a mailbox in the order messages were sent.
func List(l workspace.Layout, actor string) ([]Message, error) {
	paths, err := filepath.Glob(filepath.Join(l.Mailbox(actor), "*.md"))
	if err != nil {
		return nil, err
	}
	out := make([]Message, 0, len(paths))
	for _, path := range paths {
		m, err := load(path)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
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
func Read(l workspace.Layout, actor, id string) (Message, []byte, error) {
	path := filepath.Join(l.Mailbox(actor), id+".md")
	front, body, err := format.LoadDocument(path)
	if err != nil {
		return Message{}, nil, fmt.Errorf("%s is not in %s's mailbox", id, actor)
	}
	var m Message
	if err := format.DecodeLoose(front, &m); err != nil {
		return Message{}, nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, body, nil
}

// AttachmentBytes returns one attachment's bytes.
func AttachmentBytes(l workspace.Layout, actor, id, name string) ([]byte, error) {
	path := filepath.Join(l.Mailbox(actor), id, filepath.Base(name))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s has no attachment %s", id, name)
	}
	return data, nil
}

// Purge removes a message and its attachments.
//
// Nothing collects messages on its own, because nothing can know which of them still
// matter: removing one is a decision, and it is the recipient's.
func Purge(l workspace.Layout, actor, id string) error {
	path := filepath.Join(l.Mailbox(actor), id+".md")
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("%s is not in %s's mailbox", id, actor)
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(l.Mailbox(actor), id)); err != nil {
		return err
	}
	return nil
}

// Sent is when a message was written, in the form the field carries.
func Sent(now time.Time) string { return now.UTC().Format(format.RFC3339UTC) }

func load(path string) (Message, error) {
	front, _, err := format.LoadDocument(path)
	if err != nil {
		return Message{}, err
	}
	var m Message
	if err := format.DecodeLoose(front, &m); err != nil {
		return Message{}, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

// Summary is one line for a list, which is what a person reads.
func (m Message) Summary() string {
	subject := strings.TrimSpace(m.Subject)
	if subject == "" {
		subject = "(no subject)"
	}
	return fmt.Sprintf("%s  %s → %s  %s", m.ID, m.From, subject, m.Sent)
}
