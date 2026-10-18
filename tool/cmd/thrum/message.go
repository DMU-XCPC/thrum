package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multiagentworkspace/thrum/tool/internal/actors"
	"github.com/multiagentworkspace/thrum/tool/internal/daemon"
	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
	"github.com/multiagentworkspace/thrum/tool/internal/messages"
	"github.com/multiagentworkspace/thrum/tool/internal/records"
)

// maxAttachmentBytes is the tested bound for one message's attachments. Larger
// payloads belong in the store, where a tag names them durably.
const maxAttachmentBytes = 10 << 20

func newMessageCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "message", Short: "Send another actor a message, and read your own"}

	var subject, bodyFile, job, replyTo, idFlag string
	var attach []string
	var wait time.Duration
	send := &cobra.Command{
		Use:   "send <actor>",
		Short: "Push a message to the site that takes it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := readBody(bodyFile)
			if err != nil {
				return err
			}
			files := map[string][]byte{}
			var total int64
			for _, spec := range attach {
				src, name := spec, ""
				if i := strings.LastIndexByte(spec, ':'); i >= 0 {
					src, name = spec[:i], spec[i+1:]
				}
				st, err := os.Stat(src)
				if err != nil {
					return err
				}
				if st.IsDir() {
					return fmt.Errorf("%q is a directory; tar it first", src)
				}
				total += st.Size()
				if total > maxAttachmentBytes {
					return fmt.Errorf("the attachments total %d bytes; a message carries at most %d; put the larger ones in the store (`thrum asset ingest`, `thrum asset publish`)", total, maxAttachmentBytes)
				}
				data, err := os.ReadFile(src)
				if err != nil {
					return err
				}
				if name == "" {
					name = filepath.Base(src)
				} else {
					name = filepath.Base(name)
				}
				if name == "" || name == "." || name == ".." || name == string(filepath.Separator) {
					return fmt.Errorf("%q is not a file name", name)
				}
				if _, dup := files[name]; dup {
					return fmt.Errorf("attachment %q is given twice; name one with <src>:<name>", name)
				}
				files[name] = data
			}
			id, err := sendMessage(workspaceRoot(), args[0], subject, body, files, job, replyTo, idFlag, wait)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), id)
			return nil
		},
	}
	send.Flags().StringVar(&subject, "subject", "", "one line, for a person reading a list")
	send.Flags().StringVar(&bodyFile, "body", "", "the body, from a file or - for stdin")
	send.Flags().StringArrayVar(&attach, "attach", nil, "a file to carry with the message")
	send.Flags().StringVar(&job, "job", "", "the job it is about, if any")
	send.Flags().StringVar(&replyTo, "reply-to", "", "the id it answers")
	send.Flags().StringVar(&idFlag, "id", "", "reuse an id to make a retry idempotent")
	send.Flags().DurationVar(&wait, "wait", 0, "retry for up to this long while the site is unreachable")

	var watch bool
	list := &cobra.Command{
		Use:   "list",
		Short: "This actor's mailbox, oldest first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := workspaceRoot()
			seen := map[string]bool{}
			for {
				msgs, err := messages.List(l, actorName())
				if err != nil {
					return err
				}
				for _, m := range msgs {
					if seen[m.ID] {
						continue
					}
					seen[m.ID] = true
					if jsonFlag {
						if err := printJSONLine(cmd, m); err != nil {
							return err
						}
						continue
					}
					fmt.Fprintf(cmd.OutOrStdout(), "%-24s %-14s %-20s %s\n", m.ID, m.From, m.Sent, m.Subject)
				}
				if !watch {
					return nil
				}
				time.Sleep(500 * time.Millisecond)
			}
		},
	}
	list.Flags().BoolVar(&watch, "watch", false, "keep reporting arrivals; the backlog is printed first")

	var outDir string
	var force bool
	read := &cobra.Command{
		Use:   "read <id>",
		Short: "Read one message, and take any files it carried",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			l := workspaceRoot()
			msg, body, err := messages.Read(l, actorName(), args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "id      %s\nfrom    %s\nsent    %s\nsubject %s\n", msg.ID, msg.From, msg.Sent, msg.Subject)
			if msg.ReplyTo != "" {
				fmt.Fprintf(out, "replies %s\n", msg.ReplyTo)
			}
			if msg.Job != "" {
				fmt.Fprintf(out, "job     %s\n", msg.Job)
			}
			fmt.Fprintf(out, "\n%s", body)
			for _, a := range msg.Attachments {
				src := filepath.Join(messages.AttachmentDir(l, actorName(), msg.ID), a.Name)
				if outDir == "" {
					fmt.Fprintf(out, "\nattachment %s (%s, %d bytes) at %s\n", a.Name, mimeOf(a), a.Size, src)
					continue
				}
				dst := filepath.Join(outDir, a.Name)
				if !force {
					if _, err := os.Stat(dst); err == nil {
						return fmt.Errorf("%s exists; pass --force to overwrite", dst)
					}
				}
				if err := copyFile(src, dst); err != nil {
					return err
				}
				fmt.Fprintf(out, "\nattachment %s -> %s\n", a.Name, dst)
			}
			return nil
		},
	}
	read.Flags().StringVar(&outDir, "out", "", "write the attachments into this directory")
	read.Flags().BoolVar(&force, "force", false, "overwrite attachments that already exist in --out")

	var replySubject, replyBody string
	reply := &cobra.Command{
		Use:   "reply <id>",
		Short: "Answer a message",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			l := workspaceRoot()
			msg, _, err := messages.Read(l, actorName(), args[0])
			if err != nil {
				return err
			}
			body, err := readBody(replyBody)
			if err != nil {
				return err
			}
			subject := replySubject
			if subject == "" {
				subject = "re: " + msg.Subject
			}
			id, err := sendMessage(l, msg.From, subject, body, nil, msg.Job, msg.ID, "", 0)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), id)
			return nil
		},
	}
	reply.Flags().StringVar(&replySubject, "subject", "", "one line, for a person reading a list")
	reply.Flags().StringVar(&replyBody, "body", "", "the body, from a file or - for stdin")

	purge := &cobra.Command{
		Use:   "purge <id>",
		Short: "Remove a message and its attachments",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := messages.Purge(workspaceRoot(), actorName(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "purged %s\n", args[0])
			return nil
		},
	}

	cmd.AddCommand(send, list, read, reply, purge)
	return cmd
}

func sendMessage(l layout.Layout, to, subject string, body []byte, files map[string][]byte, job, replyTo, id string, wait time.Duration) (string, error) {
	from := actorName()
	if _, ok, err := actors.Find(l, from); err != nil {
		return "", err
	} else if !ok {
		return "", fmt.Errorf("actor %q is not declared; declare it first", from)
	}
	decl, ok, err := actors.Find(l, to)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("no actor named %q", to)
	}

	site := decl.Home
	if p, ok := actors.Places(l).Actors[to]; ok && p.Site != "" {
		site = p.Site
	} else if placed, err := askPlace(l, to); err == nil && placed != "" {
		site = placed
	}
	if site == "" {
		return "", fmt.Errorf("where %q is cannot be established", to)
	}

	if id == "" {
		id = messages.NewID()
	}
	msg := records.Message{
		ID: id, From: from, To: to, Site: actors.Site(l),
		Sent:    time.Now().UTC().Format(format.RFC3339UTC),
		Subject: subject, ReplyTo: replyTo, Job: job,
	}
	deadline := time.Now().Add(wait)
	redirects := 0
	for {
		var err error
		if site == actors.Site(l) {
			err = messages.Send(l, msg, body, files)
			if err == nil || errors.Is(err, messages.ErrExists) {
				return msg.ID, nil
			}
		} else {
			err = forward(l, site, msg, body, files)
			if err == nil {
				return msg.ID, nil
			}
		}
		var notHere *daemon.NotHere
		if errors.As(err, &notHere) && notHere.Site != "" && notHere.Site != site && redirects < 2 {
			redirects++
			site = notHere.Site
			_ = actors.SetPlace(l, to, site, time.Now())
			continue
		}
		if wait > 0 && time.Now().Before(deadline) {
			time.Sleep(250 * time.Millisecond)
			continue
		}
		return "", err
	}
}

// forward hands a message to this site's daemon, which chooses the direct edge or
// the next hop. The daemon owns the route table; the CLI never picks a via.
func forward(l layout.Layout, site string, msg records.Message, body []byte, files map[string][]byte) error {
	encoded := map[string]string{}
	for name, data := range files {
		encoded[filepath.Base(name)] = base64.StdEncoding.EncodeToString(data)
	}
	args, err := json.Marshal(struct {
		To      string            `json:"to"`
		Message records.Message   `json:"message"`
		Body    string            `json:"body,omitempty"`
		Files   map[string]string `json:"files,omitempty"`
		From    string            `json:"from,omitempty"`
		Seen    []string          `json:"seen,omitempty"`
		Hops    int               `json:"hops,omitempty"`
	}{
		To: site, Message: msg,
		Body: base64.StdEncoding.EncodeToString(body), Files: encoded,
		From: actors.Site(l),
	})
	if err != nil {
		return err
	}
	if err := ensureDaemon(l); err != nil {
		return err
	}
	_, err = daemon.LocalDo(l, daemon.Request{Verb: "msg.forward", Args: args})
	if err == nil {
		return nil
	}
	var refusal *daemon.Refusal
	if errors.As(err, &refusal) && refusal.Code == "not_here" {
		var d struct {
			Site string `json:"site"`
		}
		if json.Unmarshal(refusal.Data, &d) == nil && d.Site != "" {
			return &daemon.NotHere{Site: d.Site}
		}
	}
	return err
}

// askPlace asks this site's daemon, which asks the peers.
func askPlace(l layout.Layout, to string) (string, error) {
	args, err := json.Marshal(struct {
		Actor string `json:"actor"`
	}{to})
	if err != nil {
		return "", err
	}
	if err := ensureDaemon(l); err != nil {
		return "", err
	}
	resp, err := daemon.LocalDo(l, daemon.Request{Verb: "actor.where", Args: args})
	if err != nil {
		return "", err
	}
	var d struct {
		Here bool   `json:"here"`
		Site string `json:"site"`
	}
	if err := json.Unmarshal(resp.Data, &d); err != nil {
		return "", err
	}
	if d.Here {
		return d.Site, nil
	}
	return "", nil
}

func readBody(file string) ([]byte, error) {
	switch file {
	case "":
		return nil, nil
	case "-":
		return io.ReadAll(os.Stdin)
	default:
		return os.ReadFile(file)
	}
}

func mimeOf(a records.Attachment) string {
	if a.MIME != "" {
		return a.MIME
	}
	return "application/octet-stream"
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}
