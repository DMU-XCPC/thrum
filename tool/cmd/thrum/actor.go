package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/multiagentworkspace/thrum/tool/internal/actors"
)

type actorView struct {
	Actor      string `json:"actor"`
	Kind       string `json:"kind"`
	About      string `json:"about,omitempty"`
	Home       string `json:"home,omitempty"`
	Registered string `json:"registered,omitempty"`
	Retired    string `json:"retired,omitempty"`
	Site       string `json:"site,omitempty"`
	Heard      string `json:"heard,omitempty"`
}

func newActorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "actor",
		Short: "Declare a name, say where its mail is, and read the register",
	}

	var kind, about, home string
	declare := &cobra.Command{
		Use:   "declare <name> --kind human|agent",
		Short: "Declare a name once",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if kind == "" {
				return fmt.Errorf("--kind is required: human or agent")
			}
			if err := actors.Declare(workspaceRoot(), args[0], kind, about, home, time.Now()); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "declared %s\n", args[0])
			return nil
		},
	}
	declare.Flags().StringVar(&kind, "kind", "", "human or agent")
	declare.Flags().StringVar(&about, "about", "", "what this actor is for, in a person's words")
	declare.Flags().StringVar(&home, "home", "", "where mail waits; defaults to this site")

	var as string
	hello := &cobra.Command{
		Use:   "hello",
		Short: "Say where this actor's mail is now",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := as
			if name == "" {
				name = actorName()
			}
			if err := actors.Hello(workspaceRoot(), name, time.Now()); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s is here\n", name)
			return nil
		},
	}
	hello.Flags().StringVar(&as, "as", "", "the actor name; defaults to --actor")

	list := &cobra.Command{
		Use:   "list",
		Short: "Every name that exists, and where each was last known",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := workspaceRoot()
			names, err := actors.List(l)
			if err != nil {
				return err
			}
			places := actors.Places(l)
			views := make([]actorView, 0, len(names))
			for _, a := range names {
				v := actorView{Actor: a.Actor, Kind: a.Kind, About: a.About, Home: a.Home,
					Registered: a.Registered, Retired: a.Retired}
				if p, ok := places.Actors[a.Actor]; ok {
					v.Site, v.Heard = p.Site, p.Heard
				}
				views = append(views, v)
			}
			if jsonFlag {
				return printJSON(cmd, views)
			}
			if len(views) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no names yet: an actor is declared, once")
				return nil
			}
			for _, v := range views {
				where := v.Site
				if where == "" {
					where = "not here"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-20s %-6s %-20s %s\n", v.Actor, v.Kind, where, v.Home)
			}
			return nil
		},
	}

	show := &cobra.Command{
		Use:   "show <name>",
		Short: "Everything known about one name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			l := workspaceRoot()
			a, ok, err := actors.Find(l, args[0])
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("no actor named %q", args[0])
			}
			v := actorView{Actor: a.Actor, Kind: a.Kind, About: a.About, Home: a.Home,
				Registered: a.Registered, Retired: a.Retired}
			if p, ok := actors.Places(l).Actors[a.Actor]; ok {
				v.Site, v.Heard = p.Site, p.Heard
			}
			if jsonFlag {
				return printJSON(cmd, v)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "name       %s\nkind       %s\n", v.Actor, v.Kind)
			if v.About != "" {
				fmt.Fprintf(out, "about      %s\n", v.About)
			}
			if v.Home != "" {
				fmt.Fprintf(out, "home       %s\n", v.Home)
			}
			if v.Registered != "" {
				fmt.Fprintf(out, "registered %s\n", v.Registered)
			}
			if v.Retired != "" {
				fmt.Fprintf(out, "retired    %s\n", v.Retired)
			}
			if v.Site != "" {
				fmt.Fprintf(out, "last heard %s, at %s\n", v.Heard, v.Site)
			}
			return nil
		},
	}

	retire := &cobra.Command{
		Use:   "retire <name>",
		Short: "Retire a name; it is never reused",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := actors.Retire(workspaceRoot(), args[0], time.Now()); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "retired %s\n", args[0])
			return nil
		},
	}

	cmd.AddCommand(declare, hello, list, show, retire)
	return cmd
}

func printJSON(cmd *cobra.Command, v any) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// printJSONLine writes one compact value on one line, which is what a stream is.
func printJSONLine(cmd *cobra.Command, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return err
}
