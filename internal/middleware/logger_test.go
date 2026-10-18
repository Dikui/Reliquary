package middleware

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/types"
)

func TestSanitizeBody(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "camelCase apiKey",
			in:   `{"modelName":"gpt-5.2","apiKey":"sk-secret-123","provider":"azure_openai"}`,
			want: `{"modelName":"gpt-5.2","apiKey":"***","provider":"azure_openai"}`,
		},
		{
			name: "snake_case api_key",
			in:   `{"api_key":"sk-secret-123"}`,
			want: `{"api_key":"***"}`,
		},
		{
			name: "PascalCase APIKey",
			in:   `{"APIKey":"sk-secret-123"}`,
			want: `{"APIKey":"***"}`,
		},
		{
			name: "secretKey camelCase",
			in:   `{"secretKey":"abc","accessKeyId":"id"}`,
			want: `{"secretKey":"***","accessKeyId":"id"}`,
		},
		{
			name: "refreshToken / accessToken camelCase",
			in:   `{"refreshToken":"rt","accessToken":"at"}`,
			want: `{"refreshToken":"***","accessToken":"***"}`,
		},
		{
			name: "password and token preserved as masked",
			in:   `{"password":"p","token":"t"}`,
			want: `{"password":"***","token":"***"}`,
		},
		{
			name: "snake_case new_password and old_password",
			in:   `{"email":"alice@example.com","new_password":"FreshPass9","old_password":"OldPass9"}`,
			want: `{"email":"alice@example.com","new_password":"***","old_password":"***"}`,
		},
		{
			name: "extra whitespace around colon",
			in:   `{"apiKey"  :   "leak"}`,
			want: `{"apiKey":"***"}`,
		},
		{
			name: "non sensitive fields untouched",
			in:   `{"baseUrl":"https://example.com","modelName":"gpt"}`,
			want: `{"baseUrl":"https://example.com","modelName":"gpt"}`,
		},
		{
			name: "OAuth authorization response fields",
			in:   `{"authorization_url":"https://idp.example/authorize?state=secret","authorization_attempt":"secret-state"}`,
			want: `{"authorization_url":"***","authorization_attempt":"***"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeBody(tc.in)
			if got != tc.want {
				t.Errorf("sanitizeBody(%q)\n got: %s\nwant: %s", tc.in, got, tc.want)
			}
		})
	}
}

func TestSanitizeQuery(t *testing.T) {
	got := sanitizeQuery("code=secret-code&state=secret-state&next=%2Fsettings&state=second")
	want := "code=%2A%2A%2A&next=%2Fsettings&state=%2A%2A%2A"
	if got != want {
		t.Fatalf("sanitizeQuery() = %q, want %q", got, want)
	}
}

func TestSanitizeQueryOmitsOAuthNonceVerifierAndIDTokenHint(t *testing.T) {
	got := sanitizeQuery("nonce=secret-nonce&code_verifier=secret-verifier&id_token_hint=secret-id&state=secret-state&account_id=safe-account")
	want := "account_id=safe-account&code_verifier=%2A%2A%2A&id_token_hint=%2A%2A%2A&nonce=%2A%2A%2A&state=%2A%2A%2A"
	if got != want {
		t.Fatalf("sanitizeQuery() = %q, want %q", got, want)
	}
}

func TestLoggerOmitsRejectedIngestAndDiagnosticMaterialBodies(t *testing.T) {
	var output bytes.Buffer
	base := logger.GetLogger(context.Background()).Logger
	previousOutput, previousLevel, previousFormatter := base.Out, base.GetLevel(), base.Formatter
	logger.SetOutput(&output)
	logger.SetLogLevel(logger.LevelInfo)
	base.SetFormatter(&logrus.JSONFormatter{})
	t.Cleanup(func() {
		logger.SetOutput(previousOutput)
		base.SetLevel(previousLevel)
		base.SetFormatter(previousFormatter)
	})
	const original = `{"items":[{"content":"private-original-fixture","images":[{"data":"private-image-fixture"}]}],"private-error-fixture":"unsupported"}`
	r := gin.New()
	r.Use(RequestID(), Logger(), ErrorHandler())
	r.POST("/api/v1/ingest", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.Equal(t, original, string(body), "logging must preserve the handler input")
		_, err = types.DecodeIngestBatch(bytes.NewReader(body))
		require.ErrorContains(t, err, "private-error-fixture")
		c.Error(errors.NewBadRequestError(err.Error()))
	})
	r.GET("/api/v1/diagnostics/ingest-failures/failure/materials/input", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"content": "private-diagnostic-fixture"})
	})
	r.GET("/health", func(c *gin.Context) { c.String(http.StatusOK, "public-response-fixture") })
	r.GET("/public-error", func(c *gin.Context) { c.Error(errors.NewBadRequestError("public-error-fixture")) })
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/api/v1/ingest", strings.NewReader(original)),
		httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/ingest-failures/failure/materials/input", nil),
		httptest.NewRequest(http.MethodGet, "/health", nil),
		httptest.NewRequest(http.MethodGet, "/public-error", nil),
	} {
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		r.ServeHTTP(recorder, request)
		if request.URL.Path == "/api/v1/ingest" || request.URL.Path == "/public-error" {
			require.Equal(t, http.StatusBadRequest, recorder.Code)
		} else {
			require.Equal(t, http.StatusOK, recorder.Code)
		}
	}
	logs := output.String()
	require.Contains(t, logs, "/api/v1/ingest")
	require.Contains(t, logs, "/api/v1/diagnostics/ingest-failures/failure/materials/input")
	require.Contains(t, logs, "public-response-fixture", "the assertion must observe real access logs")
	require.Contains(t, logs, `"error":{"code":1000}`, "private errors retain their structured code")
	require.Contains(t, logs, "public-error-fixture", "ordinary paths retain their original error log")
	for _, private := range []string{"private-original-fixture", "private-image-fixture", "private-diagnostic-fixture", "private-error-fixture"} {
		require.NotContains(t, logs, private)
	}
}
