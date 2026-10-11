package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"reliquary/internal/middleware"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type multipartKnowledgeService struct {
	interfaces.KnowledgeService
	metadata map[string]string
	config   *types.KnowledgeProcessOverrides
	body     string
	fileName string
}

func (s *multipartKnowledgeService) CreateKnowledgeFromFile(_ context.Context, _ string, file *multipart.FileHeader, metadata map[string]string, _ *bool, fileName, _ string, config *types.KnowledgeProcessOverrides) (*types.Knowledge, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	s.metadata, s.config, s.body, s.fileName = metadata, config, string(body), fileName
	return &types.Knowledge{ID: "uploaded", KnowledgeBaseID: "kb-1"}, nil
}

func TestKnowledgeFileMultipartAcceptsSourceAndProcessJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &multipartKnowledgeService{}
	h := &KnowledgeHandler{kbService: &stubArticleKBService{}, kgService: service}
	router := gin.New()
	router.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		c.Set(types.SpaceIDContextKey.String(), uint64(1))
		ctx := context.WithValue(c.Request.Context(), types.SpaceIDContextKey, uint64(1))
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	router.POST("/api/v1/knowledge-bases/:id/knowledge/file", h.CreateKnowledgeFromFile)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("file", "article.md")
	require.NoError(t, err)
	_, err = file.Write([]byte("# Complete sourced Markdown\n\nOriginal article body."))
	require.NoError(t, err)
	source := types.SourceInfo{Author: "Multipart author", Publisher: "Multipart publisher", OriginalURL: "https://example.com/article", PublishedAt: "2026-10-11"}
	chunkSize := 512
	config := types.KnowledgeProcessOverrides{ChunkingConfig: &types.ChunkingConfig{ChunkSize: chunkSize, ChunkOverlap: 32, ChunkOverlapSet: true}}
	for key, value := range map[string]any{"source_info": source, "process_config": config} {
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		require.NoError(t, form.WriteField(key, string(encoded)))
	}
	require.NoError(t, form.WriteField("fileName", "folder/article.md"))
	require.NoError(t, form.Close())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-bases/kb-1/knowledge/file", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var result struct {
		Success bool            `json:"success"`
		Data    types.Knowledge `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.True(t, result.Success)
	require.Equal(t, "uploaded", result.Data.ID)
	require.Equal(t, source.Author, service.metadata["author"])
	require.Equal(t, source.Publisher, service.metadata["publisher"])
	require.Equal(t, source.OriginalURL, service.metadata["original_url"])
	require.Equal(t, source.PublishedAt, service.metadata["published_at"])
	require.Equal(t, config, *service.config)
	require.Equal(t, "folder/article.md", service.fileName)
	require.Equal(t, "# Complete sourced Markdown\n\nOriginal article body.", service.body)
}
