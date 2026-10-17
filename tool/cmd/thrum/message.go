package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/multiagentworkspace/thrum/tool/internal/actors"
	"github.com/multiagentworkspace/thrum/tool/internal/daemon"
	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
	"github.com/multiagentworkspace/thrum/tool/internal/messages"
	"github.com/multiagentworkspace/thrum/tool/internal/records"
)

func newMessageCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "message", Short: "Send another actor a message, and read your own"}

	var subject, bodyFile, job, replyTo, idFlag string
	var attach []string
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
			for _, p := range attach {
				data, err := os.ReadFile(p)
				if err != nil {
					return err
				}
				files[filepath.Base(p)] = data
			}
			id, err := sendMessage(workspaceRoot(), args[0], subject, body, files, job, replyTo, idFlag)
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

	list := &cobra.Command{
		Use:   "list",
		Short: "This actor's mailbox, oldest first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			msgs, err := messages.List(workspaceRoot(), actorName())
			if err != nil {
				return err
			}
			if jsonFlag {
				for _, m := range msgs {
					if err := printJSONLine(cmd, m); err != nil {
						return err
					}
				}
				return nil
			}
			for _, m := range msgs {
				fmt.Fprintf(cmd.OutOrStdout(), "%-24s %-14s %-20s %s\n", m.ID, m.From, m.Sent, m.Subject)
			}
			return nil
		},
	}

	var outDir string
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
					fmt.Fprintf(out, "\nattachment %s (%d bytes) at %s\n", a.Name, a.Size, src)
					continue
				}
				dst := filepath.Join(outDir, a.Name)
				if err := copyFile(src, dst); err != nil {
					return err
				}
				fmt.Fprintf(out, "\nattachment %s -> %s\n", a.Name, dst)
			}
			return nil
		},
	}
	read.Flags().StringVar(&outDir, "out", "", "write the attachments into this directory")

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
			id, err := sendMessage(l, msg.From, subject, body, nil, msg.Job, msg.ID, "")
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

func sendMessage(l layout.Layout, to, subject string, body []byte, files map[string][]byte, job, replyTo, id string) (string, error) {
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
	if site == actors.Site(l) {
		if err := messages.Send(l, msg, body, files); err != nil {
			if errors.Is(err, messages.ErrExists) {
				return msg.ID, nil
			}
			return "", err
		}
		return msg.ID, nil
	}
	if err := daemon.Push(l, site, msg, body, files); err != nil {
		return "", err
	}
	return msg.ID, nil
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

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}
