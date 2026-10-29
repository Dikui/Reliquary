package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLibraryColorHTTP(t *testing.T) {
	f := newMCPFixture(t)
	read := func(body []byte) map[string]any {
		var response struct {
			Data map[string]any `json:"data"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		return response.Data
	}
	var id string
	for i, expected := range []float64{6, 7, 8, 1, 2, 3, 4, 5, 6, 7} {
		created := personalRequest(t, f.router, http.MethodPost, "/api/v1/libraries",
			fmt.Sprintf(`{"directory":"color-%d","name":"Color %d"}`, i, i), f.key, true)
		require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
		data := read(created.Body.Bytes())
		require.Equal(t, expected, data["color"])
		id = data["id"].(string)
		loaded := personalRequest(t, f.router, http.MethodGet, "/api/v1/libraries/"+id, "", f.key, true)
		require.Equal(t, http.StatusOK, loaded.Code, loaded.Body.String())
		require.Equal(t, expected, read(loaded.Body.Bytes())["color"])
	}
	for _, method := range []string{http.MethodPut, http.MethodPatch} {
		changed := personalRequest(t, f.router, method, "/api/v1/libraries/"+id, `{"color":8}`, f.key, true)
		require.Equal(t, http.StatusOK, changed.Code, changed.Body.String())
		require.Equal(t, float64(8), read(changed.Body.Bytes())["color"])
		unchanged := personalRequest(t, f.router, method, "/api/v1/libraries/"+id,
			`{"description":"Keep chosen color"}`, f.key, true)
		require.Equal(t, http.StatusOK, unchanged.Code, unchanged.Body.String())
		require.Equal(t, float64(8), read(unchanged.Body.Bytes())["color"])
	}
	explicit := personalRequest(t, f.router, http.MethodPost, "/api/v1/libraries",
		`{"directory":"chosen-color","name":"Chosen color","color":3}`, f.key, true)
	require.Equal(t, http.StatusCreated, explicit.Code, explicit.Body.String())
	require.Equal(t, float64(3), read(explicit.Body.Bytes())["color"])
	for _, value := range []string{"0", "9", "-1", "1.5", `"2"`} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch} {
			path, body := "/api/v1/libraries/"+id, `{"color":`+value+`}`
			if method == http.MethodPost {
				path, body = "/api/v1/libraries", `{"directory":"invalid-color","name":"Invalid","color":`+value+`}`
			}
			invalid := personalRequest(t, f.router, method, path, body, f.key, true)
			require.Equal(t, http.StatusBadRequest, invalid.Code, invalid.Body.String())
			require.Contains(t, invalid.Body.String(), "color")
		}
	}
	// The compatibility settings endpoint updates the oldest library.
	legacy := personalRequest(t, f.router, http.MethodPut, "/api/v1/library",
		`{"name":"Legacy color","color":4}`, f.key, true)
	require.Equal(t, http.StatusOK, legacy.Code, legacy.Body.String())
	require.Equal(t, float64(4), read(legacy.Body.Bytes())["color"])
	legacy = personalRequest(t, f.router, http.MethodPut, "/api/v1/library", `{"name":"Legacy unchanged"}`, f.key, true)
	require.Equal(t, http.StatusOK, legacy.Code, legacy.Body.String())
	require.Equal(t, float64(4), read(legacy.Body.Bytes())["color"])
	legacy = personalRequest(t, f.router, http.MethodPut, "/api/v1/library",
		`{"name":"Invalid","color":0}`, f.key, true)
	require.Equal(t, http.StatusBadRequest, legacy.Code, legacy.Body.String())
	require.Contains(t, legacy.Body.String(), "color")
	listed := personalRequest(t, f.router, http.MethodGet, "/api/v1/libraries", "", f.key, true)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	var list struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(listed.Body.Bytes(), &list))
	for _, library := range list.Data {
		color, ok := library["color"].(float64)
		require.True(t, ok)
		require.GreaterOrEqual(t, color, float64(1))
		require.LessOrEqual(t, color, float64(8))
	}
}
