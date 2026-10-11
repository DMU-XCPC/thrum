package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/multiagentworkspace/thrum/tool/internal/daemon"
	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/messages"
	"github.com/multiagentworkspace/thrum/tool/internal/roster"
	"github.com/multiagentworkspace/thrum/tool/internal/watch"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
	"github.com/spf13/cobra"
)

// Messages are how actors reach each other. They are process information rather than
// records: never committed, never validated, and kept until somebody removes them.
//
// Every command here is one a person types, and an agent's own tooling can wrap them —
// which is why every one of them can print JSON, and why an id and a subject are
// separate things: a machine refers to the id, and a person reads the subject.

// newMessageCmd is how actors reach each other: one mailbox per name, one file per message.
//
// The names are the objects and the verbs are the actions — `message send`, `message list`,
// `message read`, `message reply`, `message purge` — because "inbox" is a place and "purge" says
// nothing about what is being purged.
func newMessageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "message",
		Short: "Send another actor a message, read your own, and keep the mailbox in order",
		Long: "A message is process information: not a record, never committed, and never validated. It\n" +
			"goes into the recipient's mailbox at whichever site that is, or the send fails and says\n" +
			"so — delivered or refused, never queued.",
	}
	cmd.AddCommand(newSendCmd(), newListCmd(), newReadCmd(), newReplyCmd(), newPurgeCmd())
	return cmd
}

func newSendCmd() *cobra.Command {
	var (
		to      string
		subject string
		body    string
		files   []string
		jobID   string
		replyTo string
	)

	cmd := &cobra.Command{
		Use:   "send --to <actor> --subject <text> [--file <path>]",
		Short: "Send another actor a message, here or at its own site",
		Long: "It is delivered or refused, and never queued: a recipient whose site cannot be\n" +
			"reached is an error, because a workspace that assumes delivery loses messages\n" +
			"quietly. A message cannot be retracted once sent, so a correction is another\n" +
			"message with --reply-to.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := layout()
			message := messages.Message{
				ID:      messages.NewID(),
				From:    actor,
				To:      to,
				Site:    roster.Site(l),
				Sent:    messages.Sent(now()),
				ReplyTo: replyTo,
				Subject: subject,
				Job:     jobID,
			}
			contents := map[string][]byte{}
			for _, path := range files {
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				name := filepath.Base(path)
				contents[name] = data
				message.Attachments = append(message.Attachments, messages.Attachment{
					Name: name,
					Size: int64(len(data)),
				})
			}
			where, err := deliver(l, message, body, contents)
			if err != nil {
				return err
			}
			fmt.Printf("%s delivered to %s at %s\n", message.ID, to, where)
			return nil
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "the actor to send it to (required)")
	cmd.Flags().StringVar(&subject, "subject", "", "one line saying what it is about (required)")
	cmd.Flags().StringVar(&body, "body", "", "the message itself")
	cmd.Flags().StringArrayVar(&files, "file", nil, "a file to carry with it; repeatable")
	cmd.Flags().StringVar(&jobID, "job", "", "a job id, as a topic marker")
	cmd.Flags().StringVar(&replyTo, "reply-to", "", "the id of the message this answers")
	_ = cmd.MarkFlagRequired("to")
	_ = cmd.MarkFlagRequired("subject")
	return cmd
}

// oneLine is one message as a line for a person: its id, who sent it, when, and what it
// is about. The id is what every other command takes.
func oneLine(m messages.Message) string {
	reply := ""
	if m.ReplyTo != "" {
		reply = "  (reply to " + m.ReplyTo + ")"
	}
	return fmt.Sprintf("%-20s %-16s %s  %s%s", m.ID, m.From, m.Sent, m.Subject, reply)
}

// newListCmd is the mailbox: what has arrived, and optionally what keeps arriving.
func newListCmd() *cobra.Command {
	var (
		asJSON   bool
		watching bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List this actor's mailbox, or keep reporting what arrives in it",
		Long: "Nothing is deleted and nothing is marked read: a message is process information\n" +
			"that stays until somebody removes it, and which of them still matter is not\n" +
			"something a tool can know.\n\n" +
			"--watch is for a harness that has to be woken rather than to ask. It prints the\n" +
			"mailbox it finds — the only chance to see what arrived while nobody was\n" +
			"watching, since nothing is re-delivered — then one line per message as it\n" +
			"arrives, and does not exit. With --json each line is one message, so the stream\n" +
			"is JSON Lines and a reader takes one object per line.\n\n" +
			"A mailbox is created by the first message delivered into it, so the watch follows\n" +
			"the way down to it: it reports by event where the system reports changes to a\n" +
			"directory, and by looking again where it does not.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := layout()
			out := cmd.OutOrStdout()
			seen := map[string]bool{}
			emit := func(m messages.Message) error {
				seen[m.ID] = true
				if asJSON {
					line, err := json.Marshal(m)
					if err != nil {
						return err
					}
					_, err = fmt.Fprintf(out, "%s\n", line)
					return err
				}
				_, err := fmt.Fprintln(out, oneLine(m))
				return err
			}

			list, err := messages.List(l, actor)
			if err != nil {
				return err
			}
			if !watching {
				if asJSON {
					return printJSON(cmd, list)
				}
				if len(list) == 0 {
					fmt.Fprintln(out, "nothing")
					return nil
				}
				for _, m := range list {
					fmt.Fprintln(out, oneLine(m))
				}
				return nil
			}

			for _, m := range list {
				if err := emit(m); err != nil {
					return err
				}
			}
			// Deepest first: the mailbox itself once it exists, and the directories that
			// have to gain it before it can. A workspace's var/ is created with the
			// workspace, so there is always something to watch.
			follower, err := watch.Follow([]string{l.Mailbox(actor), l.Mailboxes(), l.Var()})
			if err != nil {
				return err
			}
			defer follower.Close()

			for {
				select {
				case <-cmd.Context().Done():
					return nil
				case _, live := <-follower.Changed():
					if !live {
						return fmt.Errorf("stopped watching %s", l.Mailbox(actor))
					}
				}
				ids, err := messages.IDs(l, actor)
				if err != nil {
					return err
				}
				for _, id := range ids {
					if seen[id] {
						continue
					}
					m, _, err := messages.Read(l, actor, id)
					if err != nil {
						continue // it was removed between the listing and the read
					}
					if err := emit(m); err != nil {
						return err
					}
				}
			}
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the mailbox as JSON, for other tooling")
	cmd.Flags().BoolVar(&watching, "watch", false, "keep reporting messages as they arrive, and never exit")
	return cmd
}

func newReadCmd() *cobra.Command {
	var (
		asJSON bool
		out    string
	)

	cmd := &cobra.Command{
		Use:   "read <id>",
		Short: "Read one message, and take its attachments if it has any",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			l := layout()
			m, body, err := messages.Read(l, actor, args[0])
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(cmd, struct {
					messages.Message
					Body string `json:"body"`
				}{m, string(body)})
			}
			fmt.Printf("id       %s\n", m.ID)
			fmt.Printf("from     %s at %s\n", m.From, m.Site)
			fmt.Printf("sent     %s\n", m.Sent)
			fmt.Printf("subject  %s\n", m.Subject)
			if m.ReplyTo != "" {
				fmt.Printf("reply to %s\n", m.ReplyTo)
			}
			if m.Job != "" {
				fmt.Printf("job      %s\n", m.Job)
			}
			fmt.Println()
			fmt.Println(strings.TrimSpace(string(body)))
			for _, a := range m.Attachments {
				fmt.Printf("\nattachment %s (%d bytes)\n", a.Name, a.Size)
				if out == "" {
					continue
				}
				data, err := messages.AttachmentBytes(l, actor, m.ID, a.Name)
				if err != nil {
					return err
				}
				if err := os.MkdirAll(out, 0o755); err != nil {
					return err
				}
				path := filepath.Join(out, filepath.Base(a.Name))
				if err := os.WriteFile(path, data, 0o644); err != nil {
					return err
				}
				fmt.Printf("           saved to %s\n", path)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the message as JSON, for other tooling")
	cmd.Flags().StringVar(&out, "out", "", "save attachments into this directory")
	return cmd
}

func newReplyCmd() *cobra.Command {
	var (
		body  string
		files []string
	)

	cmd := &cobra.Command{
		Use:   "reply <id>",
		Short: "Answer a message, naming what it answers",
		Long: "A reply is an ordinary message whose reply_to names the one it answers: there is no\n" +
			"separate kind, because there is nothing else a reply does.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			l := layout()
			original, _, err := messages.Read(l, actor, args[0])
			if err != nil {
				return err
			}
			message := messages.Message{
				ID:      messages.NewID(),
				From:    actor,
				To:      original.From,
				Site:    roster.Site(l),
				Sent:    messages.Sent(now()),
				ReplyTo: original.ID,
				Subject: "re: " + original.Subject,
				Job:     original.Job,
			}
			contents := map[string][]byte{}
			for _, path := range files {
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				name := filepath.Base(path)
				contents[name] = data
				message.Attachments = append(message.Attachments, messages.Attachment{Name: name, Size: int64(len(data))})
			}
			where, err := deliver(l, message, body, contents)
			if err != nil {
				return err
			}
			fmt.Printf("%s delivered to %s at %s\n", message.ID, message.To, where)
			return nil
		},
	}
	cmd.Flags().StringVar(&body, "body", "", "the reply itself")
	cmd.Flags().StringArrayVar(&files, "file", nil, "a file to carry with it; repeatable")
	return cmd
}

func newPurgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "purge <id>",
		Short: "Remove a message and its attachments from this mailbox",
		Long: "Nothing collects messages, because nothing can know which of them still matter.\n" +
			"Removing one is a decision, and it is the recipient's.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			l := layout()
			if err := messages.Purge(l, actor, args[0]); err != nil {
				return err
			}
			fmt.Printf("%s purged\n", args[0])
			return nil
		},
	}
	return cmd
}

// deliver puts a message in the recipient's mailbox: here if the recipient is here, and
// otherwise by asking the site that has it.
//
// An unknown name is refused rather than written into a mailbox nobody reads, because a
// typo that silently goes nowhere is worse than an error: the roster is exactly the
// knowledge that makes this check possible.
func deliver(l workspace.Layout, m messages.Message, body string, files map[string][]byte) (string, error) {
	entry, ok, silent, older := findActor(l, m.To)
	if !ok {
		return "", noSuchName(m.To, silent, older, "the send")
	}
	if err := roster.Seen(l, m.From, m.Job, now()); err != nil {
		return "", err
	}

	here := roster.Site(l)
	if entry.Site == "" || entry.Site == here {
		if err := messages.Deliver(l, m, body, files); err != nil {
			return "", err
		}
		return here, nil
	}

	// Another site, and only its daemon can write into its mailboxes.
	peers := daemon.NewPeers(l)
	ref, err := peers.Lookup(entry.Site)
	if err != nil {
		return "", err
	}
	if err := ensureServing(l); err != nil {
		return "", err
	}
	payload := make([]byte, 0)
	for _, a := range m.Attachments {
		payload = append(payload, files[a.Name]...)
	}
	header, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	resp, err := peers.Ask(ref, daemon.Request{
		Verb: daemon.VerbDeliver,
		Args: daemon.Args(map[string]any{
			"message":     json.RawMessage(header),
			"body":        body,
			"attachments": m.Attachments,
		}),
		Files: payload,
	})
	if err != nil {
		return "", fmt.Errorf("%s did not take the message for %s, so the send failed rather than being queued: %w", entry.Site, m.To, err)
	}
	if resp.Code == daemon.CodeUnknownVerb {
		return "", fmt.Errorf("%s does not answer %s, so it is a different version of the tooling: the send failed, and nothing was queued", entry.Site, daemon.VerbDeliver)
	}
	if !resp.OK {
		return "", fmt.Errorf("%s refused the message for %s, so the send failed rather than being queued: %s", entry.Site, m.To, resp.Error)
	}
	return entry.Site, nil
}

// tellWaiters sends a message to whoever was waiting on a job that has finished.
//
// Nothing keeps a list of waiters, because the waiting is already written down: a job
// whose latest handoff is `blocked` on this one, and a job whose brief declares it in
// `depends_on`. The message goes to that job's owner, and when it has none, to whoever
// worked it last — the actor most likely to pick it up. A job nobody has ever touched
// gets no message, and that is said out loud rather than passed over.
func tellWaiters(cmd *cobra.Command, finished string) {
	l := layout()
	for _, wait := range waitingOn(l, finished) {
		to := whoToTell(l, wait)
		if to == "" {
			fmt.Fprintf(cmd.ErrOrStderr(),
				"warning: %s was waiting on %s and has no owner and no history, so nobody was told\n", wait, finished)
			continue
		}
		message := messages.Message{
			ID:      messages.NewID(),
			From:    actor,
			To:      to,
			Site:    roster.Site(l),
			Sent:    messages.Sent(now()),
			Subject: finished + " is done",
			Job:     wait,
		}
		body := finished + " is no longer something to wait for, and " + wait + " was waiting on it.\n"
		where, err := deliver(l, message, body, nil)
		if err != nil {
			// Telling somebody is a courtesy to the next actor, not part of finishing
			// this job: the record is already written, and the wait is already on
			// record for whoever reads it next.
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not tell %s that %s is done: %v\n", to, finished, err)
			continue
		}
		fmt.Printf("told %s at %s that %s is done\n", to, where, finished)
	}
}

// waitingOn lists the jobs whose record says they are waiting on a finished one.
func waitingOn(l workspace.Layout, finished string) []string {
	paths, _ := filepath.Glob(filepath.Join(l.Task(), "*", "brief.md"))
	var out []string
	for _, path := range paths {
		job := filepath.Base(filepath.Dir(path))
		if job == finished {
			continue
		}
		if waitsByBrief(l, job, finished) || waitsByHandoff(l, job, finished) {
			out = append(out, job)
		}
	}
	return out
}

// waitsByBrief is the declaration a job makes before it starts work.
func waitsByBrief(l workspace.Layout, job, finished string) bool {
	front, _, err := format.LoadDocument(l.Brief(job))
	if err != nil {
		return false
	}
	var brief format.Brief
	if err := format.DecodeLoose(front, &brief); err != nil {
		return false
	}
	for _, dep := range brief.DependsOn {
		if dep == finished {
			return true
		}
	}
	return false
}

// waitsByHandoff is what the last stretch said it was blocked on.
func waitsByHandoff(l workspace.Layout, job, finished string) bool {
	paths, _ := filepath.Glob(filepath.Join(l.HandoffDir(job), "*.md"))
	if len(paths) == 0 {
		return false
	}
	sort.Strings(paths)
	front, _, err := format.LoadDocument(paths[len(paths)-1])
	if err != nil {
		return false
	}
	var h format.Handoff
	if err := format.DecodeLoose(front, &h); err != nil {
		return false
	}
	if h.Status != format.HandoffBlocked {
		return false
	}
	for _, dep := range h.BlockedBy {
		if dep == finished {
			return true
		}
	}
	return false
}

// whoToTell is the job's owner, or whoever worked it last.
func whoToTell(l workspace.Layout, job string) string {
	if front, _, err := format.LoadDocument(l.Brief(job)); err == nil {
		var brief format.Brief
		if err := format.DecodeLoose(front, &brief); err == nil && brief.Owner != "" {
			return brief.Owner
		}
	}
	paths, _ := filepath.Glob(filepath.Join(l.HandoffDir(job), "*.md"))
	if len(paths) == 0 {
		return ""
	}
	sort.Strings(paths)
	front, _, err := format.LoadDocument(paths[len(paths)-1])
	if err != nil {
		return ""
	}
	var h format.Handoff
	if err := format.DecodeLoose(front, &h); err != nil {
		return ""
	}
	return h.Agent
}
