package stream

import "reliquary/internal/types/interfaces"

// NewStreamManager creates the process-local event stream manager.
func NewStreamManager() (interfaces.StreamManager, error) {
	return NewMemoryStreamManager(), nil
}
