package main

import (
	"fmt"

	"github.com/multiagentworkspace/ws/tool/internal/assets"
	"github.com/spf13/cobra"
)

// newAssetCmd is the asset plane: bytes in a bare repository, content-addressed, and
// published as tags. Assets are never checked out permanently.
func newAssetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "asset",
		Short: "Put durable data in the store, publish it, and take it out again",
		Long: "Publishing a version is a tag, which makes it atomic: the blobs are written first,\n" +
			"and the tag is what makes the dataset visible. A tag is never moved, and never\n" +
			"deleted except by withdrawal.",
	}

	var (
		asset    string
		version  string
		path     string
		object   string
		license  string
		source   string
		jobID    string
		commitID string
		summary  string
		date     string
		reason   string
		superBy  string
		prune    bool
	)

	ingest := &cobra.Command{
		Use:   "ingest",
		Short: "Write a file or a directory into the store, without publishing it",
		Long: "The bytes land in the store's object database and stay unreachable until a tag\n" +
			"publishes them, which is what makes publishing atomic. A single file gets a raw\n" +
			"checksum as well, because anything crossing the workspace boundary speaks in raw\n" +
			"checksums rather than git object ids.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			got, err := assets.Ingest(layout(), path)
			if err != nil {
				return err
			}
			fmt.Printf("object %s\n", got.Commit)
			if got.Single {
				fmt.Printf("sha256 %s\nbytes %d\n", got.SHA256, got.Bytes)
			} else {
				fmt.Println("content is a directory, so it has no single raw checksum")
			}
			return nil
		},
	}
	ingest.Flags().StringVar(&path, "path", "", "the file or directory to ingest (required)")
	_ = ingest.MarkFlagRequired("path")

	publish := &cobra.Command{
		Use:   "publish",
		Short: "Tag a version and write its manifest",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := assets.Publish(layout(), assets.PublishOptions{
				Asset:   asset,
				Version: version,
				Object:  object,
				Path:    path,
				License: license,
				Source:  source,
				Job:     jobID,
				Commit:  commitID,
				Summary: summary,
			})
			if err != nil {
				return err
			}
			fmt.Printf("published %s (%s)\n", asset+"@"+version, short(id))
			return nil
		},
	}
	publish.Flags().StringVar(&asset, "asset", "", "the asset's name (required)")
	publish.Flags().StringVar(&version, "version", "", "the version: a calendar date, or date-N (required)")
	publish.Flags().StringVar(&path, "path", "", "ingest these bytes and publish them")
	publish.Flags().StringVar(&object, "object", "", "publish an object already ingested")
	publish.Flags().StringVar(&license, "license", "", "the terms that govern reuse")
	publish.Flags().StringVar(&source, "source", "", "where externally sourced data came from")
	publish.Flags().StringVar(&jobID, "job", "", "the job this version came from (required)")
	publish.Flags().StringVar(&commitID, "commit", "", "the commit it came from (default: the job's branch)")
	publish.Flags().StringVar(&summary, "summary", "", "the commit's summary")
	_ = publish.MarkFlagRequired("asset")
	_ = publish.MarkFlagRequired("version")
	_ = publish.MarkFlagRequired("job")

	materialise := &cobra.Command{
		Use:   "materialise",
		Short: "Unpack a version into a job's transient data directory",
		Long: "Assets are never checked out permanently. A job materialises the version it needs\n" +
			"into its own payload, where the retention policy applies to it like anything else.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			count, err := assets.Materialise(layout(), asset, version, jobID)
			if err != nil {
				return err
			}
			fmt.Printf("unpacked %d files into job/%s/data\n", count, jobID)
			return nil
		},
	}
	materialise.Flags().StringVar(&asset, "asset", "", "the asset's name (required)")
	materialise.Flags().StringVar(&version, "version", "", "the version (required)")
	materialise.Flags().StringVar(&jobID, "job", "", "the job to unpack it for (required)")
	_ = materialise.MarkFlagRequired("asset")
	_ = materialise.MarkFlagRequired("version")
	_ = materialise.MarkFlagRequired("job")

	withdraw := &cobra.Command{
		Use:   "withdraw",
		Short: "Retire a version and delete its tag",
		Long: "Withdrawal is a retirement rather than a deletion: the manifest moves to the archive\n" +
			"with a notice, so a handoff that names the version keeps a resolvable reference and\n" +
			"the reason is on record. The version number stays taken.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := assets.Withdraw(layout(), assets.WithdrawOptions{
				Asset:        asset,
				Version:      version,
				Date:         date,
				Reason:       reason,
				SupersededBy: superBy,
				Summary:      summary,
			})
			if err != nil {
				return err
			}
			fmt.Printf("withdrew %s (%s)\n", asset+"@"+version, short(id))
			return nil
		},
	}
	withdraw.Flags().StringVar(&asset, "asset", "", "the asset's name (required)")
	withdraw.Flags().StringVar(&version, "version", "", "the version (required)")
	withdraw.Flags().StringVar(&date, "date", "", "the retirement date, as a calendar date (required)")
	withdraw.Flags().StringVar(&reason, "reason", "", "why the version must not be used (required)")
	withdraw.Flags().StringVar(&superBy, "superseded-by", "", "the doc-relative path of what replaces it")
	withdraw.Flags().StringVar(&summary, "summary", "", "the commit's summary")
	_ = withdraw.MarkFlagRequired("asset")
	_ = withdraw.MarkFlagRequired("version")
	_ = withdraw.MarkFlagRequired("date")
	_ = withdraw.MarkFlagRequired("reason")

	store := &cobra.Command{
		Use:   "store",
		Short: "Report what no tag reaches, which is regenerable by definition",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			found, err := assets.Unreachable(layout())
			if err != nil {
				return err
			}
			for _, f := range found {
				fmt.Println(f)
			}
			if len(found) == 0 {
				fmt.Println("everything in the store is reachable from a tag")
				return nil
			}
			fmt.Printf("%d objects no tag reaches\n", len(found))
			if prune {
				if err := assets.Prune(layout()); err != nil {
					return err
				}
				fmt.Println("pruned")
			} else {
				fmt.Println("(they may be collected; ask for --prune to do it now)")
			}
			return nil
		},
	}
	store.Flags().BoolVar(&prune, "prune", false, "remove them now")

	cmd.AddCommand(ingest, publish, materialise, withdraw, store)
	return cmd
}
