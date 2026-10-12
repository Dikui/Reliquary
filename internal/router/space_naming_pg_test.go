package router

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/queue"
	"reliquary/internal/types"
)

func TestSpaceHTTPNamesAndAPIKeySettingsRoundtrip(t *testing.T) {
	f := newPersonalAuthFixture(t)
	for _, credential := range []struct {
		key string
		api bool
	}{{f.token, false}, {f.key, true}} {
		response := personalRequest(t, f.router, http.MethodPut, "/api/v1/space", `{"name":"Naming roundtrip","description":"Preserved setting"}`, credential.key, credential.api)
		require.Equal(t, 200, response.Code, response.Body.String())
		response = personalRequest(t, f.router, http.MethodGet, "/api/v1/space", "", credential.key, credential.api)
		require.Equal(t, 200, response.Code)
		var packet struct{ Data map[string]any }
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &packet))
		require.Equal(t, float64(1), packet.Data["space_id"])
		require.Equal(t, "Naming roundtrip", packet.Data["name"])
		require.NotContains(t, packet.Data, "tenant_id")
	}
}

func TestQueuedLegacyPayloadExecutesWithCurrentSpaceContext(t *testing.T) {
	f := newPersonalAuthFixture(t)
	client := queue.NewClient(f.db)
	defer client.Close()
	mux := queue.NewServeMux()
	mux.Use(backgroundTaskMiddleware())
	done := make(chan uint64, 2)
	mux.HandleFunc(types.TypeDocumentProcess, func(ctx context.Context, task *queue.Task) error {
		var payload types.DocumentProcessPayload
		if err := json.Unmarshal(task.Payload(), &payload); err != nil {
			return err
		}
		require.Equal(t, types.SingleSpaceID, types.MustSpaceIDFromContext(ctx))
		done <- payload.SpaceID
		return nil
	})
	for _, payload := range []string{`{"tenant_id":1,"entry_id":"legacy"}`, `{"space_id":1,"entry_id":"current"}`} {
		_, err := client.Enqueue(queue.NewTask(types.TypeDocumentProcess, []byte(payload)))
		require.NoError(t, err)
	}
	require.NoError(t, client.Start(context.Background(), mux, queue.Config{Pools: types.DefaultWorkerPoolConcurrency()}))
	for i := 0; i < 2; i++ {
		select {
		case id := <-done:
			require.Equal(t, types.SingleSpaceID, id)
		case <-time.After(15 * time.Second):
			t.Fatal("queued task did not execute")
		}
	}
}
