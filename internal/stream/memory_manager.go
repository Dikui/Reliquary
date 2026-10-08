package stream

import (
	"context"
	"sync"
	"time"

	"reliquary/internal/types/interfaces"
)

// Streams expire one hour after their latest event, matching the previous production retention.
const streamRetention = time.Hour

type memoryStreamData struct {
	events      []interfaces.StreamEvent
	lastUpdated time.Time
}

// MemoryStreamManager stores append-only events for this backend process.
type MemoryStreamManager struct {
	streams map[string]map[string]*memoryStreamData
	mu      sync.RWMutex
	now     func() time.Time
}

func NewMemoryStreamManager() *MemoryStreamManager {
	return &MemoryStreamManager{streams: make(map[string]map[string]*memoryStreamData), now: time.Now}
}

func (m *MemoryStreamManager) AppendEvent(_ context.Context, sessionID, messageID string, event interfaces.StreamEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	if m.streams[sessionID] == nil {
		m.streams[sessionID] = make(map[string]*memoryStreamData)
	}
	stream := m.streams[sessionID][messageID]
	if stream == nil || !now.Before(stream.lastUpdated.Add(streamRetention)) {
		stream = &memoryStreamData{}
		m.streams[sessionID][messageID] = stream
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = now
	}
	stream.events = append(stream.events, event)
	stream.lastUpdated = now
	return nil
}

func (m *MemoryStreamManager) GetEvents(_ context.Context, sessionID, messageID string, fromOffset int) ([]interfaces.StreamEvent, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	stream := m.streams[sessionID][messageID]
	if stream == nil || !m.now().Before(stream.lastUpdated.Add(streamRetention)) || fromOffset >= len(stream.events) {
		return []interfaces.StreamEvent{}, fromOffset, nil
	}
	events := append([]interfaces.StreamEvent(nil), stream.events[fromOffset:]...)
	return events, len(stream.events), nil
}

// CleanupExpired releases idle event buffers and empty session maps.
func (m *MemoryStreamManager) CleanupExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	for sessionID, messages := range m.streams {
		for messageID, stream := range messages {
			if !now.Before(stream.lastUpdated.Add(streamRetention)) {
				delete(messages, messageID)
			}
		}
		if len(messages) == 0 {
			delete(m.streams, sessionID)
		}
	}
}

// RunCleanup exits when the application resource cleaner cancels its context.
func (m *MemoryStreamManager) RunCleanup(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.CleanupExpired()
		}
	}
}

var _ interfaces.StreamManager = (*MemoryStreamManager)(nil)
