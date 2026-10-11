package types

import "testing"

const testAESKey32 = "0123456789abcdef0123456789abcdef"

func withAESKey(t *testing.T, key string) { t.Helper(); t.Setenv("SYSTEM_AES_KEY", key) }
