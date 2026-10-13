package service

import (
	"context"

	"fmt"

	"strings"

	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// unknownFileType is returned by getFileType when a name carries no extension.
const unknownFileType = "unknown"

// supportedImportFileExtensions defines original file types accepted by ingest.
var supportedImportFileExtensions = map[string]struct{}{
	"pdf": {}, "txt": {}, "docx": {}, "doc": {}, "epub": {},
	"md": {}, "markdown": {},
	"png": {}, "jpg": {}, "jpeg": {}, "gif": {}, "bmp": {}, "tiff": {}, "webp": {},
	"csv": {}, "xlsx": {}, "xls": {}, "pptx": {}, "ppt": {}, "json": {},
	"docm": {}, "odt": {}, "rtf": {}, "pptm": {}, "odp": {}, "xlsm": {}, "ods": {},
}

// dataTableFileExtensions are the spreadsheet formats that get an extra
// table-summary task after their document-process task.
var dataTableFileExtensions = map[string]struct{}{
	"csv": {}, "xlsx": {}, "xls": {},
}

// normalizeFileExtension lowercases an extension and strips a leading dot so
// callers can pass either "xlsx", ".XLSX", or a raw user-supplied file_type.
func normalizeFileExtension(ext string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))
}

// isSupportedImportExtension reports whether a bare extension can be imported.
func isSupportedImportExtension(ext string) bool {
	ext = normalizeFileExtension(ext)
	if ext == "" || ext == unknownFileType {
		return false
	}
	_, ok := supportedImportFileExtensions[ext]
	return ok
}

// isDataTableFileType reports whether an extension is a spreadsheet format.
func isDataTableFileType(ext string) bool {
	_, ok := dataTableFileExtensions[normalizeFileExtension(ext)]
	return ok
}

// validateImportFileType applies the extension constraints shared by every
// file import path and reports a user-facing reason when one is violated.
func validateImportFileType(fileType string) error {
	fileType = normalizeFileExtension(fileType)
	if fileType == "" || fileType == unknownFileType {
		return werrors.NewBadRequestError("无法确定文件类型")
	}
	if IsAudioType(fileType) {
		return werrors.NewBadRequestError(fmt.Sprintf("不支持上传音频文件: %s", fileType))
	}
	if IsVideoType(fileType) {
		return werrors.NewBadRequestError("暂不支持上传视频文件")
	}
	if !isSupportedImportExtension(fileType) {
		return werrors.NewBadRequestError(fmt.Sprintf("不支持的文件类型: %s", fileType))
	}
	return nil
}

// getFileType extracts the file extension from a filename
func getFileType(filename string) string {
	ext := strings.Split(filename, ".")
	if len(ext) < 2 {
		return unknownFileType
	}
	return ext[len(ext)-1]
}

func (s *entryService) resolveFileService(ctx context.Context, kb *types.KnowledgeBase) interfaces.FileService {
	return s.fileSvc
}

func (s *entryService) resolveFileServiceForPath(ctx context.Context, kb *types.KnowledgeBase, filePath string) interfaces.FileService {
	return s.fileSvc
}

// IsAudioType checks if a file type is an audio format
func IsAudioType(fileType string) bool {
	switch strings.ToLower(fileType) {
	case "mp3", "wav", "m4a", "flac", "ogg", "aac":
		return true
	default:
		return false
	}
}

// IsVideoType checks if a file type is a video format
func IsVideoType(fileType string) bool {
	switch strings.ToLower(fileType) {
	case "mp4", "mov", "avi", "mkv", "webm", "wmv", "flv":
		return true
	default:
		return false
	}
}

func IsImageType(fileType string) bool {
	switch fileType {
	case "jpg", "jpeg", "png", "gif", "webp", "bmp", "svg", "tiff":
		return true
	default:
		return false
	}
}
