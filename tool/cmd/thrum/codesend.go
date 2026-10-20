package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newCodeCmd groups the source-snapshot transfer.
func newCodeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "code",
		Short: "Move a job's source snapshot between sites",
	}
	cmd.AddCommand(newCodeSendCmd())
	return cmd
}

// newCodeSendCmd copies one job's working tree to a helper. It is the old
// `sync --code`, with the records left to `sync`.
func newCodeSendCmd() *cobra.Command {
	var site string
	cmd := &cobra.Command{
		Use:   "send <job> --site <helper>",
		Short: "Copy a job's source snapshot to a helper",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if site == "" {
				return fmt.Errorf("--site is required")
			}
			return copyCode(workspaceRoot(), site, args[0])
		},
	}
	cmd.Flags().StringVar(&site, "site", "", "the helper site")
	return cmd
}
