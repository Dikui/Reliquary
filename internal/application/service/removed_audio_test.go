package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

func TestCreateKnowledgeFromFileRejectsAudioBeforeSaving(t *testing.T) {
	for _, extension := range []string{"mp3", "wav", "m4a", "flac", "ogg", "aac"} {
		t.Run(extension, func(t *testing.T) {
			repo := &createKnowledgeFileRepoStub{}
			files := &createKnowledgeFileServiceStub{}
			tasks := &createKnowledgeTaskEnqueuerStub{}
			svc := &knowledgeService{
				repo: repo, fileSvc: files, task: tasks,
				kbService: &createKnowledgeFileKBServiceStub{kb: &types.KnowledgeBase{ID: "kb-1"}},
			}
			knowledge, err := svc.CreateKnowledgeFromFile(newCreateKnowledgeFileContext(), "kb-1",
				newMultipartFileHeader(t, "recording."+extension, "audio bytes"), nil, nil, "", nil, "", nil)
			require.Nil(t, knowledge)
			var appErr *apperrors.AppError
			require.ErrorAs(t, err, &appErr)
			require.Equal(t, apperrors.ErrBadRequest, appErr.Code)
			require.Equal(t, "不支持上传音频文件: "+extension, appErr.Message)
			require.Zero(t, files.saveCalls)
			require.Zero(t, repo.createCalls)
			require.Zero(t, tasks.calls)
		})
	}
}

func TestTemporaryDocumentRejectsAudioBeforeSaving(t *testing.T) {
	svc := &temporaryDocumentService{}
	document, err := svc.Create(context.Background(), 1, "session-1", "recording.wav", "audio/wav", 5,
		strings.NewReader("audio"), types.TemporaryDocumentCreateOptions{})
	require.Nil(t, document)
	require.EqualError(t, err, "不支持上传音频文件: wav")
}

func TestAudioURLUsesFileImportRejection(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "example.com")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	for _, extension := range []string{"mp3", "wav", "m4a", "flac", "ogg", "aac"} {
		t.Run(extension, func(t *testing.T) {
			repo := &createKnowledgeFileRepoStub{}
			tasks := &createKnowledgeTaskEnqueuerStub{}
			svc := &knowledgeService{
				repo: repo, task: tasks,
				kbService: &createKnowledgeFileKBServiceStub{kb: &types.KnowledgeBase{ID: "kb-1"}},
			}
			knowledge, err := svc.CreateKnowledgeFromURL(newCreateKnowledgeFileContext(), "kb-1",
				"https://example.com/recording."+extension, "", "", nil, "", nil, "", nil)
			require.Nil(t, knowledge)
			var appErr *apperrors.AppError
			require.ErrorAs(t, err, &appErr)
			require.Equal(t, apperrors.ErrBadRequest, appErr.Code)
			require.Equal(t, "不支持上传音频文件: "+extension, appErr.Message)
			require.Zero(t, repo.createCalls)
			require.Zero(t, tasks.calls)
		})
	}
}

func TestModelServiceRejectsRemovedModelType(t *testing.T) {
	svc := &modelService{}
	for _, modelType := range []types.ModelType{"ASR", "asr"} {
		t.Run(string(modelType), func(t *testing.T) {
			model := &types.Model{Type: modelType}
			for _, err := range []error{svc.CreateModel(context.Background(), model), svc.UpdateModel(context.Background(), model)} {
				var appErr *apperrors.AppError
				require.ErrorAs(t, err, &appErr)
				require.Equal(t, apperrors.ErrValidation, appErr.Code)
				require.Equal(t, "不支持的模型类型: "+string(modelType), appErr.Message)
			}
		})
	}
}
