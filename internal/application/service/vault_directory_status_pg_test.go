package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func vaultStatusLibrary(t *testing.T, status VaultStatus, directory string) VaultLibraryStatus {
	t.Helper()
	for _, library := range status.Libraries {
		if library.Directory == directory {
			return library
		}
	}
	t.Fatalf("missing library directory %s", directory)
	return VaultLibraryStatus{}
}

func TestVaultStatusListsAllLibrariesAndCountsArchiveRecords(t *testing.T) {
	h := newVaultDocumentHarness(t, 0)
	require.NoError(t, h.db.Model(h.library).Update("name", "官方信息").Error)
	custom := &types.Library{ID: uuid.NewString(), SpaceID: 1, Name: "自建资料", Directory: "custom-vault", Template: types.TemplateOwn, Type: types.LibraryTypeDocument}
	require.NoError(t, h.db.Create(custom).Error)
	for _, directory := range []string{"official", "media", "custom-vault"} {
		var library types.Library
		require.NoError(t, h.db.Where("directory=?", directory).First(&library).Error)
		for _, path := range []string{directory + "/archived.md", ""} {
			require.NoError(t, h.db.Create(&types.Entry{ID: uuid.NewString(), SpaceID: 1, LibraryID: library.ID, Title: "Archive fixture", Type: types.EntryTypeManual, SourceKind: "article", ParseStatus: types.ParseStatusCompleted, VaultPath: path}).Error)
		}
	}
	status, err := h.documents.Status(h.ctx)
	require.NoError(t, err)
	require.Len(t, status.Libraries, 6)
	for _, defaults := range types.SystemLibraryDefaults() {
		row := vaultStatusLibrary(t, status, defaults.Directory)
		require.Equal(t, defaults.Name, row.Name)
		require.Equal(t, defaults.SystemRole, row.SystemRole)
		var expected int64
		if defaults.Directory == "official" || defaults.Directory == "media" {
			expected = 1
		}
		require.Equal(t, expected, row.ArchivedEntries)
	}
	require.EqualValues(t, 1, vaultStatusLibrary(t, status, "custom-vault").ArchivedEntries)
	require.Equal(t, custom.Name, vaultStatusLibrary(t, status, "custom-vault").Name)
	localized, err := h.documents.Status(context.WithValue(h.ctx, types.LanguageContextKey, "en-US"))
	require.NoError(t, err)
	require.Equal(t, "Official information", vaultStatusLibrary(t, localized, "official").Name)
	require.Equal(t, custom.Name, vaultStatusLibrary(t, localized, "custom-vault").Name)
}
