package errors

import "net/http"

// NewImageProcessingModelUnsupportedError reports an incompatible image model.
func NewImageProcessingModelUnsupportedError() *AppError {
	return &AppError{Code: ErrImageProcessingModelUnsupported, Message: "图片处理模型必须是 VLLM 模型或已开启支持视觉/多模态的对话模型", HTTPCode: http.StatusBadRequest}
}

// NewImageProcessingModelNotFoundError reports a missing selected image model.
func NewImageProcessingModelNotFoundError(modelID string) *AppError {
	return &AppError{Code: ErrImageProcessingModelNotFound, Message: "图片处理模型不存在", Details: map[string]string{"model_id": modelID}, HTTPCode: http.StatusBadRequest}
}

// NewImageProcessingModelUnavailableError reports unavailable model validation.
func NewImageProcessingModelUnavailableError() *AppError {
	return &AppError{Code: ErrImageProcessingModelUnavailable, Message: "图片处理模型服务不可用", HTTPCode: http.StatusBadRequest}
}
