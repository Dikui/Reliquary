package service

import (
	"context"

	"strings"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// unknownFileType is returned by getFileType when a name carries no extension.
const unknownFileType = "unknown"

// normalizeFileExtension lowercases an extension and strips a leading dot so
// callers can pass either "xlsx", ".XLSX", or a raw user-supplied file_type.
func normalizeFileExtension(ext string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))
}

// getFileType extracts the file extension from a filename
func getFileType(filename string) string {
	ext := strings.Split(filename, ".")
	if len(ext) < 2 {
		return unknownFileType
	}
	return ext[len(ext)-1]
}

func (s *entryService) resolveFileService(ctx context.Context, library *types.Library) interfaces.FileService {
	return s.fileSvc
}

func (s *entryService) resolveFileServiceForPath(ctx context.Context, library *types.Library, filePath string) interfaces.FileService {
	return s.fileSvc
}

func IsImageType(fileType string) bool {
	switch fileType {
	case "jpg", "jpeg", "png", "gif", "webp", "bmp", "svg", "tiff":
		return true
	default:
		return false
	}
}
