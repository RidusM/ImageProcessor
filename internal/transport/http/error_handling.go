package httpt

import (
	"errors"
	"net/http"

	"img-processor/internal/entity"
	"github.com/gin-gonic/gin"
	"github.com/wb-go/wbf/logger"
)

func (h *ImageHandler) handleServiceError(c *gin.Context, op string, err error) {
	ctx := c.Request.Context()
	log := h.log.Ctx(ctx).With("op", op, "error", err)

	switch {
	case errors.Is(err, entity.ErrImageNotFound),
		errors.Is(err, entity.ErrFileNotFound),
		errors.Is(err, entity.ErrTaskNotFound):
		log.LogAttrs(ctx, logger.WarnLevel, "resource not found")
		h.respondError(c, http.StatusNotFound, "not_found", "Resource not found", nil)

	case errors.Is(err, entity.ErrImageAlreadyExists):
		log.LogAttrs(ctx, logger.WarnLevel, "resource already exists")
		h.respondError(c, http.StatusConflict, "already_exists", "Resource already exists", nil)

	case errors.Is(err, entity.ErrInvalidImageFormat),
		errors.Is(err, entity.ErrUnsupportedFormat),
		errors.Is(err, entity.ErrInvalidData):
		log.LogAttrs(ctx, logger.WarnLevel, "invalid input data")
		h.respondError(c, http.StatusBadRequest, "invalid_data", "Invalid input data", nil)

	case errors.Is(err, entity.ErrFileTooLarge):
		log.LogAttrs(ctx, logger.WarnLevel, "file too large")
		h.respondError(c, http.StatusRequestEntityTooLarge, "payload_too_large", "File exceeds size limit", nil)

	case errors.Is(err, entity.ErrProcessingFailed):
		log.LogAttrs(ctx, logger.ErrorLevel, "processing failed")
		h.respondError(c, http.StatusInternalServerError, "processing_failed", "Image processing failed", nil)

	case errors.Is(err, entity.ErrStorageUnavailable):
		log.LogAttrs(ctx, logger.ErrorLevel, "storage unavailable")
		h.respondError(c, http.StatusServiceUnavailable, "service_unavailable", "Storage service unavailable", nil)

	default:
		log.LogAttrs(ctx, logger.ErrorLevel, "internal server error")
		h.respondError(c, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
	}
}

func (h *ImageHandler) respondJSON(c *gin.Context, status int, data any) {
	c.JSON(status, data)
}

func (h *ImageHandler) respondError(c *gin.Context, status int, code, message string, err error) {
	response := ErrorResponse{
		Error: message,
		Code:  code,
	}
	if err != nil {
		response.Details = err.Error()
	}
	h.respondJSON(c, status, response)
}