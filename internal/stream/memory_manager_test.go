package stream

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types/interfaces"
)

func TestMemoryStreamConcurrentAppendAndResume(t *testing.T) {
	m, err := NewStreamManager()
	require.NoError(t, err)
	ctx := context.Background()
	var writers sync.WaitGroup
	for i := 0; i < 50; i++ {
		writers.Add(1)
		go func(i int) {
			defer writers.Done()
			require.NoError(t, m.AppendEvent(ctx, "session", "message", interfaces.StreamEvent{ID: fmt.Sprint(i)}))
		}(i)
	}
	writers.Wait()
	events, offset, err := m.GetEvents(ctx, "session", "message", 0)
	require.NoError(t, err)
	require.Len(t, events, 50)
	require.Equal(t, 50, offset)
	seen := make(map[string]bool)
	for _, event := range events {
		require.False(t, seen[event.ID])
		seen[event.ID] = true
		require.False(t, event.Timestamp.IsZero())
	}
	events[0].ID = "changed copy"
	stored, _, err := m.GetEvents(ctx, "session", "message", 0)
	require.NoError(t, err)
	require.NotEqual(t, events[0].ID, stored[0].ID)
	require.NoError(t, m.AppendEvent(ctx, "session", "message", interfaces.StreamEvent{ID: "next"}))
	resumed, next, err := m.GetEvents(ctx, "session", "message", offset)
	require.NoError(t, err)
	require.Len(t, resumed, 1)
	require.Equal(t, "next", resumed[0].ID)
	require.Equal(t, 51, next)
	other, _, err := m.GetEvents(ctx, "other", "message", 0)
	require.NoError(t, err)
	require.Empty(t, other)
}

func TestMemoryStreamRetentionRefreshAndCleanup(t *testing.T) {
	m := NewMemoryStreamManager()
	now := time.Now()
	m.now = func() time.Time { return now }
	ctx := context.Background()
	require.NoError(t, m.AppendEvent(ctx, "s", "m", interfaces.StreamEvent{ID: "first"}))
	now = now.Add(50 * time.Minute)
	require.NoError(t, m.AppendEvent(ctx, "s", "m", interfaces.StreamEvent{ID: "refresh"}))
	now = now.Add(50 * time.Minute)
	m.CleanupExpired()
	events, _, err := m.GetEvents(ctx, "s", "m", 0)
	require.NoError(t, err)
	require.Len(t, events, 2)
	now = now.Add(10 * time.Minute)
	events, _, err = m.GetEvents(ctx, "s", "m", 0)
	require.NoError(t, err)
	require.Empty(t, events)
	m.CleanupExpired()
	require.Empty(t, m.streams)
	require.NoError(t, m.AppendEvent(ctx, "s", "m", interfaces.StreamEvent{ID: "new"}))
	events, offset, err := m.GetEvents(ctx, "s", "m", 0)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "new", events[0].ID)
	require.Equal(t, 1, offset)
}
