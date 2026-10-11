package types

import (
	"bytes"
	"log"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestBuiltinModelSpaceConfigAcceptsDeprecatedKeyAndWarns(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	for _, fixture := range []struct {
		input string
		id    uint64
	}{
		{"space_id: 1", 1}, {"tenant_id: 7", 7}, {"tenant_id: 7\nspace_id: 1", 1}, {"tenant_id: 7\nspace_id: 0", 0},
	} {
		var entry BuiltinModelEntry
		require.NoError(t, yaml.Unmarshal([]byte(fixture.input), &entry))
		require.Equal(t, fixture.id, entry.SpaceID)
	}
	require.Contains(t, output.String(), "tenant_id is deprecated; use space_id")
}
