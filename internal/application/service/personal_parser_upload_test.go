package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPersonalParserUploadRejectsRemovedFormatsClearly(t *testing.T) {
	for _, ext := range []string{"html", "htm", "mhtml", "xmind"} {
		t.Run(ext, func(t *testing.T) {
			err := validateImportFileType(ext)
			require.Error(t, err)
			require.Contains(t, err.Error(), "不支持的文件类型: "+ext)
		})
	}
	for _, ext := range []string{"pdf", "docx", "md", "png"} {
		require.NoError(t, validateImportFileType(ext))
	}
}
