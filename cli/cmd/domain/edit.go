package domain

import (
	"context"
	"encoding/json"
	"github.com/spf13/cobra"
	"reliquary/cli/internal/cmdutil"
	sdk "reliquary/client"
)

type EditOptions struct {
	Name, Description, Input string
	DryRun                   bool
	Patch                    map[string]json.RawMessage
}
type EditService interface {
	GetKnowledgeDomain(context.Context, string) (*sdk.KnowledgeDomain, error)
	UpdateKnowledgeDomain(context.Context, string, *sdk.KnowledgeDomain) (*sdk.KnowledgeDomain, error)
}

func NewCmdEdit(f *cmdutil.Factory) *cobra.Command {
	opts := &EditOptions{}
	cmd := &cobra.Command{Use: "edit <domain-id>", Short: "Edit a knowledge domain template", Args: cobra.ExactArgs(1), Example: `  reliquary domain edit domain-id --input patch.json -y`, RunE: func(c *cobra.Command, args []string) error {
		fopts, err := formatOptions(c)
		if err != nil {
			return err
		}
		opts.Patch, err = readTemplatePatch(opts.Input)
		if err != nil {
			return err
		}
		if opts.Patch == nil {
			opts.Patch = map[string]json.RawMessage{}
		}
		for _, key := range []string{"name", "description"} {
			if c.Flags().Changed(key) {
				value, _ := c.Flags().GetString(key)
				opts.Patch[key], _ = json.Marshal(value)
			}
		}
		if len(opts.Patch) == 0 {
			return cmdutil.NewError(cmdutil.CodeInputInvalidArgument, "domain edit requires --name, --description or --input")
		}
		if handled, err := cmdutil.HandleDryRun(c, opts.DryRun, cmdutil.DryRunPlan{Action: "domain.edit", Args: map[string]any{"id": args[0], "template": opts.Patch}}); handled {
			return err
		}
		yes, _ := c.Flags().GetBool("yes")
		retry := cmdutil.BuildRetryArgv(c, []string{"reliquary", "domain", "edit", args[0]}, "name", "description", "input", "format")
		if err := cmdutil.ConfirmWrite(f.Prompter(), yes, fopts.WantsJSON(), "edit", "knowledge domain", args[0], "domain.edit", retry); err != nil {
			return err
		}
		client, err := f.Client()
		if err != nil {
			return err
		}
		return runEdit(c.Context(), opts, fopts, client, args[0])
	}}
	cmd.Flags().StringVar(&opts.Name, "name", "", "New name")
	cmd.Flags().StringVar(&opts.Description, "description", "", "New description")
	cmd.Flags().StringVar(&opts.Input, "input", "", "JSON template patch file, or - for stdin; omitted fields are preserved")
	cmdutil.AddDryRunFlag(cmd, &opts.DryRun)
	cmdutil.AddFormatFlag(cmd, domainFields...)
	cmdutil.SetWriteRisk(cmd, "domain.edit")
	cmdutil.SetAgentHelp(cmd, cmdutil.AgentHelp{RequiredFlags: []string{"<domain-id> (positional)"}, Output: "envelope.data is the updated KnowledgeDomain; omitted fields are preserved", UsedFor: "Edit a template while preserving omitted fields", Examples: []string{"reliquary domain edit domain-id --input patch.json -y"}})
	return cmd
}

func runEdit(ctx context.Context, opts *EditOptions, fopts *cmdutil.FormatOptions, svc EditService, id string) error {
	if len(opts.Patch) == 0 {
		return cmdutil.NewError(cmdutil.CodeInputInvalidArgument, "domain edit requires at least one template field")
	}
	current, err := svc.GetKnowledgeDomain(ctx, id)
	if err != nil {
		return cmdutil.WrapHTTP(err, "fetch knowledge domain %s", id)
	}
	if err := applyPatch(current, opts.Patch); err != nil {
		return err
	}
	updated, err := svc.UpdateKnowledgeDomain(ctx, id, current)
	if err != nil {
		return cmdutil.WrapHTTP(err, "edit knowledge domain %s", id)
	}
	return emitDomain(fopts, updated)
}
