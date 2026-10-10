package handler

import (
	"context"
	"reliquary/internal/types/interfaces"
)

const testResourceHandle = "resource://xifDo7NTSL300Lp1goVutw"

type stubResourceFileService struct {
	interfaces.FileService
	url string
}

func (s *stubResourceFileService) GetFileURL(context.Context, string) (string, error) {
	return s.url, nil
}
