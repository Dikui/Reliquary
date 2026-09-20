// Package kb holds the `reliquary kb` command tree: list / view / create /
// update / delete / pin / unpin. Verb set follows common CRUD vocabulary
// (list/view/create/update/delete) plus pin/unpin. Bulk content deletion
// is exposed via `reliquary doc delete --all --kb=<id>`.
package kb

import (
	"github.com/spf13/cobra"

	"reliquary/cli/internal/cmdutil"
)

// NewCmd builds the `reliquary kb` parent command.
func NewCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kb",
		Short: "Manage knowledge bases",
	}
	cmd.AddCommand(NewCmdList(f))
	cmd.AddCommand(NewCmdView(f))
	cmd.AddCommand(NewCmdCreate(f))
	cmd.AddCommand(NewCmdEdit(f))
	cmd.AddCommand(NewCmdDelete(f))
	cmd.AddCommand(NewCmdPin(f))
	cmd.AddCommand(NewCmdUnpin(f))
	cmd.AddCommand(NewCmdStatus(f))
	cmd.AddCommand(NewCmdCheck(f))
	cmd.AddCommand(NewCmdConfig(f)) // `config` also hosts the `config set` write subcommand
	return cmd
}
