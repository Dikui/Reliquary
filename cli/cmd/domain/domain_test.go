package domain

import (
	"context"
	"encoding/json"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"reliquary/cli/internal/cmdutil"
	"reliquary/cli/internal/iostreams"
	"reliquary/cli/internal/prompt"
	sdk "reliquary/client"
	"testing"
)

type fakeDomainService struct {
	current sdk.KnowledgeDomain
	updated *sdk.KnowledgeDomain
}

func (s *fakeDomainService) GetKnowledgeDomain(context.Context, string) (*sdk.KnowledgeDomain, error) {
	copy := s.current
	return &copy, nil
}
func (s *fakeDomainService) UpdateKnowledgeDomain(_ context.Context, _ string, d *sdk.KnowledgeDomain) (*sdk.KnowledgeDomain, error) {
	s.updated = d
	return d, nil
}
func (s *fakeDomainService) ListKnowledgeDomains(context.Context) ([]sdk.KnowledgeDomain, error) {
	return nil, nil
}

func TestEditPreservesOmittedTemplateFields(t *testing.T) {
	iostreams.SetForTest(t)
	svc := &fakeDomainService{current: sdk.KnowledgeDomain{ID: "ai", Name: "AI", Fields: []sdk.DomainField{{Key: "capacity", ValueType: "number"}}, ExtractionHints: "Preserve conflicts"}}
	opts := &EditOptions{Patch: map[string]json.RawMessage{"description": json.RawMessage(`"Updated"`)}}
	require.NoError(t, runEdit(context.Background(), opts, &cmdutil.FormatOptions{Mode: cmdutil.FormatJSON}, svc, "ai"))
	require.Equal(t, "Updated", svc.updated.Description)
	require.Equal(t, svc.current.Fields, svc.updated.Fields)
	require.Equal(t, svc.current.ExtractionHints, svc.updated.ExtractionHints)
}

func TestListEmptyUsesArrayEnvelope(t *testing.T) {
	out, _ := iostreams.SetForTest(t)
	require.NoError(t, runList(context.Background(), &cmdutil.FormatOptions{Mode: cmdutil.FormatJSON}, &fakeDomainService{}))
	var env struct {
		OK   bool                  `json:"ok"`
		Data []sdk.KnowledgeDomain `json:"data"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &env))
	require.True(t, env.OK)
	require.NotNil(t, env.Data)
}

func TestDryRunReadsPatchWithoutNetworkOrConfirmation(t *testing.T) {
	out, _ := iostreams.SetForTest(t)
	path := filepath.Join(t.TempDir(), "patch.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"fields":[{"key":"mass","value_type":"number","unit_family":"mass"}]}`), 0600))
	f := &cmdutil.Factory{Client: func() (*sdk.Client, error) { t.Fatal("unexpected network"); return nil, nil }, Prompter: func() prompt.Prompter { t.Fatal("unexpected prompt"); return nil }}
	root := &cobra.Command{Use: "reliquary"}
	root.PersistentFlags().BoolP("yes", "y", false, "")
	root.PersistentFlags().String("format", "", "")
	root.PersistentFlags().String("jq", "", "")
	root.AddCommand(NewCmd(f))
	root.SetArgs([]string{"domain", "edit", "ai", "--input", path, "--dry-run", "--format", "json"})
	require.NoError(t, root.Execute())
	require.Contains(t, out.String(), `"action":"domain.edit"`)
	require.Contains(t, out.String(), `"key":"mass"`)
}

func TestTemplateInputRejectsMetadataAndTrailingJSON(t *testing.T) {
	for _, body := range []string{`{"is_builtin":true}`, `null`, `{"name":"x"} {}`, `{"fields":"invalid"}`} {
		path := filepath.Join(t.TempDir(), "template.json")
		require.NoError(t, os.WriteFile(path, []byte(body), 0600))
		_, err := readTemplatePatch(path)
		require.Error(t, err)
	}
}
