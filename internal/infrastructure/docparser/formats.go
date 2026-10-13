package docparser

import "strings"

// IsImageFormat identifies the supported image attachment extensions.
func IsImageFormat(fileType string) bool {
	switch strings.ToLower(strings.TrimPrefix(fileType, ".")) {
	case "jpg", "jpeg", "png", "gif", "bmp", "tiff", "webp":
		return true
	}
	return false
}
