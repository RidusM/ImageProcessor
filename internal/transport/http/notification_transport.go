package handlers

import (
	"net/http"

	"img-processor/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/wb-go/wbf/logger"
)

const _maxRequestBodySize = 32 << 20

type ImageHandler struct {
	svc    *service.ProcessorService
	log    logger.Logger
	router *gin.Engine
}

func NewImageHandler(
	svc *service.ProcessorService,
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
	router.Use(h.corsMiddleware([]string{"*"}))
	router.Use(gin.Recovery())

	h.router = router

	router.Static("/static", "./static")
	router.LoadHTMLGlob("static/*.html")

	h.setupRoutes()

	return h
}

func (h *ImageHandler) Engine() *gin.Engine {
	return h.router
}
