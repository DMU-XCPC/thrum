package messages_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/gitx"

	"github.com/multiagentworkspace/thrum/tool/internal/messages"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

func fixture(t *testing.T) workspace.Layout {
	t.Helper()
	l, err := workspace.Create(workspace.CreateOptions{
		Root:  filepath.Join(t.TempDir(), "workspace"),
		Site:  "build-01",
		Actor: "tester",
	})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// A message is a file in a directory, so sending one needs nothing running, and its id
// is what everything else refers to.
func TestAMessageIsAFileWithAnOpaqueID(t *testing.T) {
	l := fixture(t)
	first := messages.NewID()
	second := messages.NewID()
	if first == second {
		t.Fatal("two ids were the same")
	}
	if len(first) < 8 {
		t.Errorf("an id should be long enough to be unique: %q", first)
	}

	m := messages.Message{
		ID:      first,
		From:    "claude-sonnet",
		To:      "claude-opus",
		Site:    "build-01",
		Sent:    messages.Sent(time.Now().UTC()),
		Subject: "the uploader timeout is explained",
	}
	if err := messages.Deliver(l, m, "It was the retry budget.\n", nil); err != nil {
		t.Fatal(err)
	}

	// The id is the file name, and the subject is not in it: an identity is for a
	// machine and a title is for a person.
	path := filepath.Join(l.Mailbox("claude-opus"), first+".md")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("a message should be one file named by its id: %v", err)
	}
	if filepath.Base(path) == m.Subject {
		t.Error("the subject is not the identity")
	}

	got, body, err := messages.Read(l, "claude-opus", first)
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != m.Subject || got.From != "claude-sonnet" {
		t.Errorf("what was read back: %+v", got)
	}
	if strings.TrimSpace(string(body)) != "It was the retry budget." {
		t.Errorf("the body changed: %q", body)
	}
}

// The bytes travel with the message and are not assets: no hash, no tag, no store.
func TestAnAttachmentIsBytesAndNothingMore(t *testing.T) {
	l := fixture(t)
	m := messages.Message{
		ID: messages.NewID(), From: "one", To: "two", Site: "build-01",
		Sent: messages.Sent(time.Now().UTC()), Subject: "the sample",
		Attachments: []messages.Attachment{{Name: "sample.csv", Size: 8}},
	}
	contents := []byte("a,b\n1,2\n")
	if err := messages.Deliver(l, m, "here it is\n", map[string][]byte{"sample.csv": contents}); err != nil {
		t.Fatal(err)
	}
	got, err := messages.AttachmentBytes(l, "two", m.ID, "sample.csv")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(contents) {
		t.Errorf("the attachment changed: %q", got)
	}
	// Nothing was published: an attachment is not an asset, and a store with no tags
	// holds nothing anybody published.
	tags, err := gitx.Run(l.Store(), "tag", "-l")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(tags) != "" {
		t.Errorf("an attachment must not become an asset: %s", tags)
	}
}

// A message cannot be retracted, and removing one is the recipient's own deliberate act.
func TestMessagesStayUntilTheyArePurged(t *testing.T) {
	l := fixture(t)
	m := messages.Message{
		ID: messages.NewID(), From: "one", To: "two", Site: "build-01",
		Sent: messages.Sent(time.Now().UTC()), Subject: "kept",
	}
	if err := messages.Deliver(l, m, "", nil); err != nil {
		t.Fatal(err)
	}
	// Delivering the same id twice is refused rather than overwriting somebody's words.
	if err := messages.Deliver(l, m, "", nil); err == nil {
		t.Error("a delivered id should not be overwritten")
	}
	if list, err := messages.List(l, "two"); err != nil || len(list) != 1 {
		t.Fatalf("the mailbox: %v %v", list, err)
	}

	if err := messages.Purge(l, "two", m.ID); err != nil {
		t.Fatal(err)
	}
	if list, err := messages.List(l, "two"); err != nil || len(list) != 0 {
		t.Errorf("the mailbox should be empty: %v %v", list, err)
	}
	if err := messages.Purge(l, "two", m.ID); err == nil {
		t.Error("purging what is not there should say so")
	}
}

// A mailbox belongs to the actor, not to the site: reading it needs no daemon, and a
// message that is not yours is not there.
func TestAMailboxBelongsToItsActor(t *testing.T) {
	l := fixture(t)
	m := messages.Message{
		ID: messages.NewID(), From: "one", To: "two", Site: "build-01",
		Sent: messages.Sent(time.Now().UTC()), Subject: "for two only",
	}
	if err := messages.Deliver(l, m, "", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := messages.Read(l, "three", m.ID); err == nil {
		t.Error("a message should only be in its recipient's mailbox")
	}
}

func TestAMessageNeedsAnIdentityASenderARecipientAndASubject(t *testing.T) {
	l := fixture(t)
	bad := []messages.Message{
		{From: "one", To: "two", Subject: "no id"},
		{ID: "m-1", To: "two", Subject: "no sender"},
		{ID: "m-2", From: "one", Subject: "no recipient"},
		{ID: "m-3", From: "one", To: "two"},
		{ID: "m-4", From: "one", To: "Not A Slug", Subject: "a bad name"},
	}
	for _, m := range bad {
		if err := messages.Deliver(l, m, "", nil); err == nil {
			t.Errorf("%+v should have been refused", m)
		}
	}
}
