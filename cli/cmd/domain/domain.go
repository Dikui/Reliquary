// Package domain manages knowledge domain templates.
package domain

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"reliquary/cli/internal/cmdutil"
	"reliquary/cli/internal/iostreams"
	sdk "reliquary/client"
)

var domainFields = []string{"id", "name", "description", "entity_types", "fields", "relations", "unit_families", "extraction_hints", "is_builtin", "created_by", "created_at", "updated_at"}

func NewCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{Use: "domain", Short: "Manage knowledge domain templates"}
	cmd.AddCommand(NewCmdList(f), NewCmdView(f), NewCmdCreate(f), NewCmdEdit(f), NewCmdDelete(f))
	return cmd
}

func formatOptions(c *cobra.Command) (*cmdutil.FormatOptions, error) {
	opts, err := cmdutil.CheckFormatFlag(c)
	if err == nil {
		opts.ResolveDefault(iostreams.IO.IsStdoutTTY())
	}
	return opts, err
}

// readTemplatePatch accepts only editable template fields, before network calls.
func readTemplatePatch(path string) (map[string]json.RawMessage, error) {
	if path == "" {
		return nil, nil
	}
	r, err := cmdutil.OpenInput(path)
	if err != nil {
		return nil, cmdutil.NewError(cmdutil.CodeInputInvalidArgument, "read domain input: "+err.Error())
	}
	if closer, ok := r.(io.Closer); ok && path != "-" {
		defer closer.Close()
	}
	decoder := json.NewDecoder(io.LimitReader(r, 4*1024*1024+1))
	var patch map[string]json.RawMessage
	if err := decoder.Decode(&patch); err != nil || patch == nil {
		return nil, cmdutil.NewError(cmdutil.CodeInputInvalidArgument, "--input must contain a JSON object")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, cmdutil.NewError(cmdutil.CodeInputInvalidArgument, "--input must contain a single JSON object")
	}
	allowed := map[string]bool{"name": true, "description": true, "entity_types": true, "fields": true, "relations": true, "unit_families": true, "extraction_hints": true}
	for key := range patch {
		if !allowed[key] {
			return nil, cmdutil.NewError(cmdutil.CodeInputInvalidArgument, "unknown domain template field: "+key)
		}
	}
	var domain sdk.KnowledgeDomain
	if err := applyPatch(&domain, patch); err != nil {
		return nil, err
	}
	return patch, nil
}

func applyPatch(domain *sdk.KnowledgeDomain, patch map[string]json.RawMessage) error {
	body, err := json.Marshal(patch)
	if err == nil {
		err = json.Unmarshal(body, domain)
	}
	if err != nil {
		return cmdutil.NewError(cmdutil.CodeInputInvalidArgument, "invalid domain template: "+err.Error())
	}
	return nil
}

func emitDomain(opts *cmdutil.FormatOptions, domain *sdk.KnowledgeDomain) error {
	if opts.WantsJSON() {
		return opts.Emit(iostreams.IO.Out, domain, nil)
	}
	w := tabwriter.NewWriter(iostreams.IO.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "ID:\t%s\nNAME:\t%s\nDESCRIPTION:\t%s\nBUILTIN:\t%t\n", domain.ID, domain.Name, domain.Description, domain.IsBuiltin)
	body, err := json.MarshalIndent(domain, "", "  ")
	if err != nil {
		return err
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(iostreams.IO.Out, string(body))
	return nil
}
