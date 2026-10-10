package main

import (
	"fmt"

	"github.com/multiagentworkspace/ws/tool/internal/bundles"
	"github.com/spf13/cobra"
)

// newBundleCmd carries one job's payload between sites.
func newBundleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bundle",
		Short: "Carry a job's payload to another site, and take one in",
		Long: "Moving a job is three acts, and only this one is the workspace's business: the\n" +
			"records and the branch travel through git, and the payload travels as a bundle —\n" +
			"the one part of a job that git cannot carry.",
	}

	var (
		jobID   string
		handoff string
		out     string
		site    string
		replace bool
	)

	export := &cobra.Command{
		Use:   "export",
		Short: "Write a job's payload as a bundle",
		Long: "Export runs after a handoff and never during work: the bundle says which handoff it\n" +
			"belongs to, and the receiver's first act is to check that it already has it. It\n" +
			"releases nothing, because stopping already released whatever the job held.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := bundles.Export(layout(), bundles.ExportOptions{
				Job:     jobID,
				Handoff: handoff,
				Out:     out,
				Site:    site,
			})
			if err != nil {
				return err
			}
			fmt.Printf("wrote %s\n", path)
			return nil
		},
	}
	export.Flags().StringVar(&jobID, "job", "", "the job to carry (required)")
	export.Flags().StringVar(&handoff, "handoff", "", "the handoff the payload belongs to (default: the most recent)")
	export.Flags().StringVar(&out, "out", "", "where to write it (default: a name in the working directory)")
	export.Flags().StringVar(&site, "site", "", "where it came from, for whoever finds the file later")
	_ = export.MarkFlagRequired("job")

	importCmd := &cobra.Command{
		Use:   "import <bundle>",
		Short: "Unpack a payload into the job it belongs to",
		Long: "Import refuses unless the records are already local: the named handoff must be\n" +
			"present, and the commit it names must resolve in the code repository. That single\n" +
			"check replaces every merge rule the workspace would otherwise need.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := bundles.Import(layout(), bundles.ImportOptions{
				Path:    args[0],
				Replace: replace,
			})
			if err != nil {
				return err
			}
			for _, p := range result.Discarded {
				fmt.Printf("discarded %s\n", p)
			}
			fmt.Printf("unpacked %d files for job %s as of %s\n",
				result.Files, result.Header.Job, result.Header.Handoff)
			if result.Header.Site != "" {
				fmt.Printf("it came from %s\n", result.Header.Site)
			}
			return nil
		},
	}
	importCmd.Flags().BoolVar(&replace, "replace", false, "discard what is there, reporting every path")

	cmd.AddCommand(export, importCmd)
	return cmd
}
