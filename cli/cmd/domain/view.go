package domain

import (
	"context"
	"github.com/spf13/cobra"
	"reliquary/cli/internal/cmdutil"
	sdk "reliquary/client"
)

type ViewService interface {
	GetKnowledgeDomain(context.Context, string) (*sdk.KnowledgeDomain, error)
}

func NewCmdView(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{Use: "view <domain-id>", Short: "View a knowledge domain template", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, args []string) error {
		opts, err := formatOptions(c)
		if err != nil {
			return err
		}
		client, err := f.Client()
		if err != nil {
			return err
		}
		return runView(c.Context(), opts, client, args[0])
	}}
	cmdutil.AddFormatFlag(cmd, domainFields...)
	cmdutil.SetAgentHelp(cmd, cmdutil.AgentHelp{RequiredFlags: []string{"<domain-id> (positional)"}, UsedFor: "View a knowledge domain template", Examples: []string{`reliquary domain view domain-id`}, Output: "envelope.data is a KnowledgeDomain with fields, relations, units and extraction hints"})
	return cmd
}

func runView(ctx context.Context, opts *cmdutil.FormatOptions, svc ViewService, id string) error {
	domain, err := svc.GetKnowledgeDomain(ctx, id)
	if err != nil {
		return cmdutil.WrapHTTP(err, "view knowledge domain %s", id)
	}
	return emitDomain(opts, domain)
}
