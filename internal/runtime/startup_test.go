package runtime

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"reliquary/internal/types"
)

func TestStartupCredentialEncryptionWarning(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
		warn bool
	}{
		{"unset", "", true},
		{"configured", "12345678901234567890123456789012", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SYSTEM_AES_KEY", tc.key)
			var output bytes.Buffer
			log := logrus.New()
			log.SetOutput(&output)
			ctx := context.WithValue(context.Background(), types.LoggerContextKey, logrus.NewEntry(log))
			LogStartupEnv(ctx)
			warning := "SYSTEM_AES_KEY is not set; MCP credentials and model API keys will be stored in plaintext"
			if got := strings.Count(output.String(), warning); got != map[bool]int{true: 1, false: 0}[tc.warn] {
				t.Fatalf("plaintext warning count = %d, want warning=%v: %s", got, tc.warn, output.String())
			}
			if tc.key != "" && strings.Contains(output.String(), tc.key) {
				t.Fatal("startup output disclosed the encryption key")
			}
		})
	}
}
