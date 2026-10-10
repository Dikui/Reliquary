package session

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type audioUploadSessionService struct{ interfaces.SessionService }

func (*audioUploadSessionService) GetOwnedSession(context.Context, string) (*types.Session, error) {
	return &types.Session{ID: "session-1"}, nil
}

func TestUploadTemporaryDocumentRejectsAudio(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "recording.wav")
	require.NoError(t, err)
	_, err = part.Write([]byte("audio bytes"))
	require.NoError(t, err)
	require.NoError(t, form.Close())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/sessions/session-1/attachments", &body)
	c.Request.Header.Set("Content-Type", form.FormDataContentType())
	c.Params = gin.Params{{Key: "session_id", Value: "session-1"}}
	h := &Handler{sessionService: &audioUploadSessionService{}}
	h.UploadTemporaryDocument(c)
	require.Len(t, c.Errors, 1)
	var appErr *apperrors.AppError
	require.ErrorAs(t, c.Errors[0].Err, &appErr)
	require.Equal(t, apperrors.ErrBadRequest, appErr.Code)
	require.Equal(t, "不支持上传音频文件: wav", appErr.Message)
}

func TestInlineAttachmentRejectsAudioBeforeSaving(t *testing.T) {
	processor := NewAttachmentProcessor(nil, nil)
	attachment, err := processor.ProcessAttachment(context.Background(), []byte("audio"), "recording.mp3", 5, 1)
	require.Nil(t, attachment)
	require.EqualError(t, err, "不支持上传音频文件: mp3")
	require.False(t, isValidFileType("recording.mp3"))
}
