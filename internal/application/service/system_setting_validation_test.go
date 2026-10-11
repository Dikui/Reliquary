package service

import "testing"

func TestValidateWorkerConcurrencyMinimums(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   any
		wantErr bool
	}{
		{name: "core zero", key: "queue.core_concurrency", value: 0, wantErr: true},
		{name: "core minimum", key: "queue.core_concurrency", value: 1},
		{name: "enrichment minimum", key: "queue.enrichment_concurrency", value: 1},
		{name: "maintenance minimum", key: "queue.maintenance_concurrency", value: 1},
		{name: "shared minimum", key: "queue.shared_concurrency", value: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRegistryEntry(tt.key, tt.value)
			if tt.wantErr && err == nil {
				t.Fatal("expected validation error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}
