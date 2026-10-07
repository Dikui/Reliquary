package storageallowlist

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllowedMap_DefaultAllowsAll(t *testing.T) {
	t.Setenv(AllowListEnv, "")
	allowed := AllowedMap()
	for _, provider := range Supported() {
		assert.True(t, allowed[provider], provider)
	}
}

func TestAllowedMap_RespectsEnv(t *testing.T) {
	t.Setenv(AllowListEnv, "minio,s3")
	allowed := AllowedMap()
	assert.True(t, allowed["minio"])
	assert.True(t, allowed["s3"])
	assert.False(t, allowed["local"])
	assert.False(t, allowed["unsupported"])
}

func TestFirstAllowed(t *testing.T) {
	t.Setenv(AllowListEnv, "minio")
	assert.Equal(t, "minio", FirstAllowed())
}

func TestAllowedList(t *testing.T) {
	t.Setenv(AllowListEnv, "s3,minio")
	assert.Equal(t, []string{"minio", "s3"}, AllowedList())
}

func TestIsAllowed_EmptyProvider(t *testing.T) {
	t.Setenv(AllowListEnv, "minio")
	require.True(t, IsAllowed(""))
}
