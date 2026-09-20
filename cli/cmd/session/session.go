// Package sessioncmd holds `reliquary session` command tree (list / view /
// delete / ask / resume / stop) for chat history and agent invocation.
//
// Package name `sessioncmd` (not `session`) so callers can `import sdk
// "reliquary/client"` and use `sdk.Session` without
// shadowing - same hygiene as `profilecmd`.
package sessioncmd

import (
	"github.com/spf13/cobra"

	"reliquary/cli/internal/cmdutil"
)

// NewCmd builds the `reliquary session` parent command.
func NewCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Manage chat sessions",
	}
	cmd.AddCommand(NewCmdList(f))
	cmd.AddCommand(NewCmdView(f))
	cmd.AddCommand(NewCmdDelete(f))
	cmd.AddCommand(NewCmdAsk(f))
	cmd.AddCommand(NewCmdResume(f))
	cmd.AddCommand(NewCmdStop(f))
	cmd.AddCommand(NewCmdToolApproval(f))
	return cmd
}
