package domain

import (
	"context"
	"encoding/json"
	"github.com/spf13/cobra"
	"reliquary/cli/internal/cmdutil"
	sdk "reliquary/client"
	"strings"
)

type CreateOptions struct {
	Name, Description, Input string
	DryRun                   bool
	Patch                    map[string]json.RawMessage
}
type CreateService interface {
	CreateKnowledgeDomain(context.Context, *sdk.KnowledgeDomain) (*sdk.KnowledgeDomain, error)
}

func NewCmdCreate(f *cmdutil.Factory) *cobra.Command {
	opts := &CreateOptions{}
	cmd := &cobra.Command{Use: "create <name>", Short: "Create a knowledge domain", Args: cobra.ExactArgs(1), Example: `  reliquary domain create "AI Research" --input template.json`, RunE: func(c *cobra.Command, args []string) error {
		fopts, err := formatOptions(c)
		if err != nil {
			return err
		}
		opts.Name = args[0]
		if strings.TrimSpace(opts.Name) == "" {
			return cmdutil.NewError(cmdutil.CodeInputInvalidArgument, "domain name is required")
		}
		opts.Patch, err = readTemplatePatch(opts.Input)
		if err != nil {
			return err
		}
		if opts.Patch == nil {
			opts.Patch = map[string]json.RawMessage{}
		}
		opts.Patch["name"], _ = json.Marshal(opts.Name)
		if c.Flags().Changed("description") {
			opts.Patch["description"], _ = json.Marshal(opts.Description)
		}
		if handled, err := cmdutil.HandleDryRun(c, opts.DryRun, cmdutil.DryRunPlan{Action: "domain.create", Args: map[string]any{"template": opts.Patch}}); handled {
			return err
		}
		client, err := f.Client()
		if err != nil {
			return err
		}
		return runCreate(c.Context(), opts, fopts, client)
	}}
	cmd.Flags().StringVar(&opts.Description, "description", "", "Domain description")
	cmd.Flags().StringVar(&opts.Input, "input", "", "JSON template file, or - for stdin")
	cmdutil.AddDryRunFlag(cmd, &opts.DryRun)
	cmdutil.AddFormatFlag(cmd, domainFields...)
	cmdutil.SetAgentHelp(cmd, cmdutil.AgentHelp{RequiredFlags: []string{"<name> (positional)"}, UsedFor: "Create a knowledge domain template", Examples: []string{`reliquary domain create "Research" --input template.json`}, Output: "envelope.data is the created KnowledgeDomain"})
	return cmd
}

func runCreate(ctx context.Context, opts *CreateOptions, fopts *cmdutil.FormatOptions, svc CreateService) error {
	if strings.TrimSpace(opts.Name) == "" {
		return cmdutil.NewError(cmdutil.CodeInputInvalidArgument, "domain name is required")
	}
	domain := &sdk.KnowledgeDomain{EntityTypes: []string{}, Fields: []sdk.DomainField{}, Relations: []sdk.DomainRelation{}, UnitFamilies: []sdk.DomainUnitFamily{}}
	if err := applyPatch(domain, opts.Patch); err != nil {
		return err
	}
	domain.Name = opts.Name
	if opts.Patch == nil {
		domain.Description = opts.Description
	}
	created, err := svc.CreateKnowledgeDomain(ctx, domain)
	if err != nil {
		return cmdutil.WrapHTTP(err, "create knowledge domain")
	}
	return emitDomain(fopts, created)
}
