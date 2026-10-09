package handler

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSystemInfoCredentialEncryptionStatus(t *testing.T) {
	for _, tc := range []struct {
		name    string
		key     string
		enabled bool
	}{
		{"unset", "", false},
		{"configured", "12345678901234567890123456789012", true},
		{"invalid key", "short", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SYSTEM_AES_KEY", tc.key)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/api/v1/system/info", nil)
			(&SystemHandler{}).GetSystemInfo(c)
			var response struct {
				Data GetSystemInfoResponse `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if w.Code != 200 || response.Data.CredentialEncryptionEnabled != tc.enabled {
				t.Fatalf("unexpected encryption state: status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
