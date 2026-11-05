package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// MEL-02: small regression/spec; absent switches are an object, explicit false
// switches survive serialization just like explicit true switches.
func TestEntityJSONAutoMatchIsObject(t *testing.T) {
	for _, tc := range []struct {
		name      string
		autoMatch map[string]bool
		want      string
	}{
		{"unset", nil, `{}`},
		{"empty", map[string]bool{}, `{}`},
		{"configured", map[string]bool{"Atlas": true, "A1": false}, `{"Atlas":true,"A1":false}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(Entity{AutoMatch: tc.autoMatch})
			require.NoError(t, err)
			var value map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(data, &value))
			require.Contains(t, value, "auto_match")
			require.JSONEq(t, tc.want, string(value["auto_match"]))
		})
	}
}
