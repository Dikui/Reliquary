package service

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestVaultStatusUsesSystemLibraryDirectories(t *testing.T) {
	h := newVaultDocumentHarness(t, 0)
	status, err := h.documents.Status(h.ctx)
	require.NoError(t, err)
	require.Equal(t, "mine", status.Mine.Directory)
	require.Equal(t, "ai-reports", status.Reports.Directory)
}
