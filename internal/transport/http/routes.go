package handler

import (
	_ "img-processor/docs" // required for Swagger

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// @title           Image Processor Service API
// @version         1.0
// @description     API для работы с обработчиком изображений
// @termsOfService  http://swagger.io/terms/
// @contact.name    RidusM
// @contact.email   stormkillpeople@gmail.com
// @license.name    MIT-0
// @license.url     https://github.com/aws/mit-0
// @host            localhost:8080
// @BasePath        /
func (h *ImageHandler) setupRoutes() {
	h.router.GET("/health", h.Health)

	h.router.POST("/upload", h.UploadImage)
	h.router.GET("/image/:id", h.GetImage)
	h.router.GET("/image/:id/status", h.GetStatus)
	h.router.GET("/images", h.ListImages)
	h.router.DELETE("/image/:id", h.DeleteImage)

	h.router.GET("/", func(c *gin.Context) {
		c.File("web/index.html")
	})

	h.router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
}
