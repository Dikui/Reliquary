package domain

import (
	"context"
	"fmt"
	"github.com/spf13/cobra"
	"reliquary/cli/internal/cmdutil"
	"reliquary/cli/internal/iostreams"
)

type DeleteService interface {
	DeleteKnowledgeDomain(context.Context, string) error
}

func NewCmdDelete(f *cmdutil.Factory) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{Use: "delete <domain-id>", Short: "Delete an unused custom domain", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, args []string) error {
		opts, err := formatOptions(c)
		if err != nil {
			return err
		}
		if handled, err := cmdutil.HandleDryRun(c, dryRun, cmdutil.DryRunPlan{Action: "domain.delete", Args: map[string]any{"id": args[0]}}); handled {
			return err
		}
		yes, _ := c.Flags().GetBool("yes")
		if err := cmdutil.ConfirmDestructive(f.Prompter(), yes, opts.WantsJSON(), "delete", "knowledge domain", args[0], "domain.delete", []string{"reliquary", "domain", "delete", args[0], "-y"}); err != nil {
			return err
		}
		client, err := f.Client()
		if err != nil {
			return err
		}
		return runDelete(c.Context(), opts, client, args[0])
	}}
	cmdutil.AddDryRunFlag(cmd, &dryRun)
	cmdutil.AddFormatFlag(cmd, "id", "deleted")
	cmdutil.SetRisk(cmd, "domain.delete")
	cmdutil.SetAgentHelp(cmd, cmdutil.AgentHelp{RequiredFlags: []string{"<domain-id> (positional)"}, Output: "envelope.data is {id, deleted:true}", UsedFor: "Delete a custom domain without knowledge bases", Examples: []string{"reliquary domain delete domain-id -y"}})
	return cmd
}

func runDelete(ctx context.Context, opts *cmdutil.FormatOptions, svc DeleteService, id string) error {
	if err := svc.DeleteKnowledgeDomain(ctx, id); err != nil {
		return cmdutil.WrapHTTP(err, "delete knowledge domain %s", id)
	}
	if opts.WantsJSON() {
		return opts.Emit(iostreams.IO.Out, map[string]any{"id": id, "deleted": true}, nil)
	}
	fmt.Fprintf(iostreams.IO.Out, "Deleted knowledge domain %s\n", id)
	return nil
}
