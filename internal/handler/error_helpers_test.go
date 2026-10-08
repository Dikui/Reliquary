package handler

import (
	"github.com/gin-gonic/gin"
	"reliquary/internal/middleware"
)

func errorCapture() gin.HandlerFunc { return middleware.ErrorHandler() }
