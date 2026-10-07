package domain

import (
	"context"
	"fmt"
	"github.com/spf13/cobra"
	"reliquary/cli/internal/cmdutil"
	"reliquary/cli/internal/iostreams"
	"reliquary/cli/internal/output"
	sdk "reliquary/client"
	"text/tabwriter"
)

type ListService interface {
	ListKnowledgeDomains(context.Context) ([]sdk.KnowledgeDomain, error)
}

func NewCmdList(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{Use: "list", Short: "List knowledge domains", Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error {
		opts, err := formatOptions(c)
		if err != nil {
			return err
		}
		client, err := f.Client()
		if err != nil {
			return err
		}
		return runList(c.Context(), opts, client)
	}}
	cmdutil.AddFormatFlag(cmd, domainFields...)
	cmdutil.SetAgentHelp(cmd, cmdutil.AgentHelp{UsedFor: "List knowledge domain templates", Examples: []string{`reliquary domain list`}, Output: "envelope.data is an array of KnowledgeDomain objects; meta.count is the list size"})
	return cmd
}

func runList(ctx context.Context, opts *cmdutil.FormatOptions, svc ListService) error {
	items, err := svc.ListKnowledgeDomains(ctx)
	if err != nil {
		return cmdutil.WrapHTTP(err, "list knowledge domains")
	}
	if items == nil {
		items = []sdk.KnowledgeDomain{}
	}
	if opts.WantsJSON() {
		return opts.Emit(iostreams.IO.Out, items, &output.Meta{Count: output.IntPtr(len(items))})
	}
	if len(items) == 0 {
		fmt.Fprintln(iostreams.IO.Out, "(no knowledge domains)")
		return nil
	}
	w := tabwriter.NewWriter(iostreams.IO.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tBUILTIN\tFIELDS")
	for _, item := range items {
		fmt.Fprintf(w, "%s\t%s\t%t\t%d\n", item.ID, item.Name, item.IsBuiltin, len(item.Fields))
	}
	return w.Flush()
}
