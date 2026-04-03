package httpt

import (
	"net/http"

	"img-processor/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/wb-go/wbf/logger"
)

const _maxRequestBodySize = 32 << 20 // 32 MB

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

	// Global limits
	router.Use(func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, _maxRequestBodySize)
	})

	// Middleware chain
	router.Use(h.requestIDMiddleware())
	router.Use(h.loggingMiddleware())
	router.Use(h.corsMiddleware([]string{"*"}))
	router.Use(gin.Recovery())

	h.router = router

	// Static files & frontend
	router.Static("/static", "./static")
	router.LoadHTMLGlob("static/*.html")

	h.setupRoutes()

	return h
}

func (h *ImageHandler) Engine() *gin.Engine {
	return h.router
}

package httpt

func (h *ImageHandler) setupRoutes() {
	// System endpoints
	h.router.GET("/health", h.Health)
	h.router.GET("/ready", h.Ready)

	// Image API
	h.router.POST("/upload", h.UploadImage)
	h.router.GET("/image/:id", h.GetImage)
	h.router.GET("/image/:id/status", h.GetStatus)
	h.router.DELETE("/image/:id", h.DeleteImage)

	// Frontend
	h.router.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", gin.H{})
	})

	// Swagger UI (если подключен gin-swagger)
	// h.router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
}