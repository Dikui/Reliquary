package diagnostics

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestFailurePackageLifecycle(t *testing.T) {
	s, err := New(t.TempDir(), DefaultRetention, DefaultMaxBytes)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.events.Close() })
	s.SetVersion("fixture@revision")
	now := time.Date(2026, 10, 18, 1, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
	ctx = context.WithValue(ctx, types.RequestIDContextKey, "request-fixture")
	input := []byte(`{"content":"甲乙丙丁"}`)
	_, a := s.Begin(ctx, input, 2, "official", "")
	image := []byte("image bytes")
	a.Add("original.png", "image/png", "asset_id:fixture", image)
	a.AddSecrets("secret-credential")
	ref := a.Record(errors.New("provider secret-credential failed"), "ingest_failed", false, "rolled_back")
	require.True(t, ref.Recorded)
	require.Equal(t, "available", ref.PackageStatus)
	f, err := s.Get(ctx, ref.FailureID)
	require.NoError(t, err)
	require.Equal(t, now.Add(7*24*time.Hour), f.ExpiresAt)
	require.NotContains(t, f.Reason, "secret-credential")
	require.Len(t, f.Materials, 2)
	text, err := s.ReadText(ctx, f.ID, f.Materials[0].ID, 13, 2)
	require.NoError(t, err)
	require.Equal(t, "乙丙", text.Content)
	require.NotNil(t, text.NextOffset)
	_, data, err := s.Read(ctx, f.ID, f.Materials[1].ID)
	require.NoError(t, err)
	require.Equal(t, image, data)
	other := context.WithValue(ctx, types.SpaceIDContextKey, uint64(2))
	_, err = s.Get(other, f.ID)
	require.ErrorIs(t, err, ErrNotFound)
	_, _, err = s.Read(ctx, f.ID, "../manifest.json")
	require.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, s.LinkRecovery(ctx, f.ID, "recovered-entry"))
	unresolved := false
	page, err := s.List(ctx, types.FailureQuery{Resolved: &unresolved})
	require.NoError(t, err)
	require.Zero(t, page.Total)
	page, err = s.List(ctx, types.FailureQuery{})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	require.True(t, page.Items[0].Resolved)
	_, data, err = s.Read(ctx, f.ID, f.Materials[0].ID)
	require.NoError(t, err)
	require.Equal(t, input, data, "recovery cannot edit the original snapshot")
	require.NoError(t, s.RemoveEntryMaterials(ctx, "recovered-entry", ""))
	f, err = s.Get(ctx, f.ID)
	require.NoError(t, err)
	require.Equal(t, "unavailable", f.PackageStatus)
	require.Empty(t, f.Materials)
	entries, err := os.ReadDir(s.failureDir(f.ID))
	require.NoError(t, err)
	require.Len(t, entries, 1, "only content-free manifest remains after confirmed rejection")

	_, a = s.Begin(ctx, input, 0, "official", "")
	ref = a.Record(errors.New("write failed"), "ingest_failed", false, "")
	f, err = s.Get(ctx, ref.FailureID)
	require.NoError(t, err)
	materialID := f.Materials[0].ID
	now = now.Add(7 * 24 * time.Hour)
	_, _, err = s.Read(ctx, f.ID, materialID)
	require.ErrorIs(t, err, ErrExpired)
	// A failed manifest write must not stop the independent expiry sweep.
	require.NoError(t, os.Mkdir(s.failureDir(uuid.NewString()), 0700))
	require.NoError(t, s.Prune())
	_, err = os.Stat(filepath.Join(s.failureDir(f.ID), materialID))
	require.True(t, os.IsNotExist(err))
	log, err := os.ReadFile(filepath.Join(s.root, "events.jsonl"))
	require.NoError(t, err)
	require.NotContains(t, string(log), "甲乙丙丁")
	require.NotContains(t, string(log), "secret-credential")
}

func TestRecoveryLookupDoesNotBlockRetention(t *testing.T) {
	s, err := New(t.TempDir(), DefaultRetention, DefaultMaxBytes)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.events.Close() })
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
	_, attempt := s.Begin(ctx, []byte(`{"content":"fixture"}`), 0, "official", "")
	attempt.Record(errors.New("write failed"), "ingest_failed", false, "")
	started, release := make(chan struct{}), make(chan struct{})
	s.SetRecoveryLookup(func(context.Context, *types.IngestFailure, []byte) (string, error) {
		close(started)
		<-release
		return "", nil
	})
	done := make(chan error, 1)
	go func() { _, err := s.List(ctx, types.FailureQuery{}); done <- err }()
	defer func() { close(release); require.NoError(t, <-done) }()
	<-started
	pruned := make(chan error, 1)
	go func() { pruned <- s.Prune() }()
	select {
	case err := <-pruned:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("database lookup blocked diagnostic retention")
	}
}

func TestFailurePackageCapacityAndWriteFailure(t *testing.T) {
	s, err := New(t.TempDir(), DefaultRetention, 1024*1024)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.events.Close() })
	s.SetVersion("fixture@revision")
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
	_, first := s.Begin(ctx, bytes.Repeat([]byte("a"), 600*1024), 0, "official", "")
	old := first.Record(errors.New("write failed"), "ingest_failed", false, "")
	_, next := s.Begin(ctx, bytes.Repeat([]byte("b"), 600*1024), 0, "official", "")
	current := next.Record(errors.New("write failed"), "ingest_failed", false, "")
	require.Equal(t, "available", current.PackageStatus)
	f, err := s.Get(ctx, old.FailureID)
	require.NoError(t, err)
	require.Equal(t, "expired", f.PackageStatus, "new capture evicts the oldest package first")
	_, oversized := s.Begin(ctx, []byte("input"), 0, "official", "")
	oversized.Add("huge.png", "image/png", "upload", bytes.Repeat([]byte("x"), 2*1024*1024))
	partial := oversized.Record(errors.New("write failed"), "ingest_failed", false, "")
	require.Equal(t, "partial", partial.PackageStatus)
	require.LessOrEqual(t, directorySize(filepath.Join(s.root, "temporary"))+directorySize(filepath.Join(s.root, "failures")), s.maxBytes)
	_, broken := s.Begin(ctx, []byte("input"), 0, "official", "")
	require.NoError(t, os.RemoveAll(broken.dir))
	require.NoError(t, os.WriteFile(broken.dir, nil, 0600))
	failed := broken.Record(errors.New("original business error"), "ingest_failed", false, "")
	require.False(t, failed.Recorded)
	require.Equal(t, "unavailable", failed.PackageStatus)
	// The package failed, but its content-free runtime event remains searchable.
	require.NoError(t, s.events.Rotate())
	page, err := s.List(ctx, types.FailureQuery{ID: failed.FailureID})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	f, err = s.Get(ctx, failed.FailureID)
	require.NoError(t, err)
	require.Equal(t, "unavailable", f.PackageStatus)
	require.Empty(t, f.Materials)
	require.Empty(t, f.Config)
	_, err = s.Get(context.WithValue(ctx, types.SpaceIDContextKey, uint64(2)), failed.FailureID)
	require.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, s.Prune())
	fragment := []byte{0x89, 0x50}
	partialUpload := s.RecordUploadFailure(ctx, "partial.png", fragment, errors.New("upload read interrupted"), false)
	require.Equal(t, "partial", partialUpload.PackageStatus)
	f, err = s.Get(ctx, partialUpload.FailureID)
	require.NoError(t, err)
	read, err := s.ReadText(ctx, f.ID, f.Materials[0].ID, 0, 0)
	require.NoError(t, err)
	require.NotEmpty(t, read.URL, "partial binary images must remain readable as original bytes")
	require.Equal(t, fragment, read.Data)
	_, data, err := s.Read(ctx, f.ID, f.Materials[0].ID)
	require.NoError(t, err)
	require.Equal(t, fragment, data)
}

func TestExpiredFailureMutationsStillRemoveStoredBytes(t *testing.T) {
	for _, operation := range []string{"link", "reject"} {
		t.Run(operation, func(t *testing.T) {
			s, err := New(t.TempDir(), DefaultRetention, DefaultMaxBytes)
			require.NoError(t, err)
			t.Cleanup(func() { _ = s.events.Close() })
			now := time.Now()
			s.now = func() time.Time { return now }
			ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
			_, a := s.Begin(ctx, []byte("private original"), 0, "official", "entry-fixture")
			ref := a.Record(errors.New("write failed"), "ingest_failed", false, "")
			// Include an interrupted file absent from the manifest.
			require.NoError(t, os.WriteFile(filepath.Join(s.failureDir(ref.FailureID), ".write-orphan"), []byte("private partial"), 0600))
			now = now.Add(DefaultRetention)
			if operation == "link" {
				err = s.LinkRecovery(ctx, ref.FailureID, "recovered-entry")
			} else {
				err = s.RemoveEntryMaterials(ctx, "entry-fixture", "")
			}
			require.NoError(t, err)
			require.NoError(t, s.Prune())
			entries, err := os.ReadDir(s.failureDir(ref.FailureID))
			require.NoError(t, err)
			require.Len(t, entries, 1, "only the content-free manifest survives expiry and rejection")
			require.Equal(t, "manifest.json", entries[0].Name())
		})
	}
}

func TestRejectionClearsInFlightCaptures(t *testing.T) {
	for _, byEntry := range []bool{false, true} {
		t.Run(map[bool]string{false: "recovery", true: "entry"}[byEntry], func(t *testing.T) {
			s, err := New(t.TempDir(), DefaultRetention, DefaultMaxBytes)
			require.NoError(t, err)
			t.Cleanup(func() { _ = s.events.Close() })
			ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
			_, parent := s.Begin(ctx, []byte("private original"), 0, "official", "entry-fixture")
			first := parent.Record(errors.New("write failed"), "ingest_failed", false, "")
			entryID := ""
			if byEntry {
				entryID = "entry-fixture"
			}
			_, active := s.Begin(ctx, []byte("private repair"), 0, "official", entryID)
			if !byEntry {
				active.SetRecoveryFailure(first.FailureID)
			}
			require.NoError(t, s.RemoveEntryMaterials(ctx, "entry-fixture", ""))
			require.Empty(t, active.dir)
			active.Add("late-model.json", "application/json", "model_call", []byte("private model"))
			late := active.Record(errors.New("private error detail"), "ingest_failed", false, "")
			failure, err := s.Get(ctx, late.FailureID)
			require.NoError(t, err)
			require.Equal(t, "unavailable", failure.PackageStatus)
			require.Empty(t, failure.Materials)
			require.NotContains(t, failure.Reason, "private error")
			files, err := os.ReadDir(s.failureDir(late.FailureID))
			require.NoError(t, err)
			require.Len(t, files, 1)
		})
	}
}

func TestUploadCaptureDuringUnrelatedRejection(t *testing.T) {
	s, err := New(t.TempDir(), DefaultRetention, DefaultMaxBytes)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.events.Close() })
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 20 {
			s.RecordUploadFailure(ctx, "partial.png", []byte{0x89, 0x50}, errors.New("upload interrupted"), false)
		}
	}()
	t.Cleanup(func() { <-done })
	for range 20 {
		require.NoError(t, s.RemoveEntryMaterials(ctx, "unrelated-entry", ""))
	}
	<-done
}
