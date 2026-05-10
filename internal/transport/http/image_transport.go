package handler

import (
	"context"
	"io"
	"net/http"

	"img-processor/internal/entity"
	"img-processor/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/wb-go/wbf/logger"
)

const _maxRequestBodySize = 32 << 20

type ImageService interface {
	Upload(ctx context.Context, req service.UploadRequest) (*service.UploadResponse, error)
	GetStatus(ctx context.Context, imageID uuid.UUID) (*entity.Task, error)
	GetImage(ctx context.Context, imageID uuid.UUID, version string) (io.ReadCloser, int64, string, error)
	ProcessTask(ctx context.Context, task *entity.Task) error
	Delete(ctx context.Context, imageID uuid.UUID) error
}

type ImageHandler struct {
	svc    ImageService
	log    logger.Logger
	router *gin.Engine
}

func NewImageHandler(
	svc ImageService,
	log logger.Logger,
) *ImageHandler {
	h := &ImageHandler{
		svc: svc,
		log: log,
	}

	router := gin.New()

	router.Use(func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, _maxRequestBodySize)
	})

	router.Use(h.requestIDMiddleware())
	router.Use(h.loggingMiddleware())
	router.Use(h.baseCORSMiddleware())
	router.Use(gin.Recovery())

	h.router = router

	h.router.Static("/static", "./web")

	h.setupRoutes()

	return h
}

func (h *ImageHandler) Engine() *gin.Engine {
	return h.router
}
