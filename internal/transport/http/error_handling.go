package handler

import (
	"errors"
	"net/http"

	"img-processor/internal/entity"

	"github.com/gin-gonic/gin"
)

func (h *ImageHandler) handleServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, entity.ErrImageNotFound),
		errors.Is(err, entity.ErrFileNotFound),
		errors.Is(err, entity.ErrTaskNotFound):
		h.respondError(c, http.StatusNotFound, "not_found",
			"Resource not found", err)
	case errors.Is(err, entity.ErrInvalidImageFormat),
		errors.Is(err, entity.ErrUnsupportedFormat):
		h.respondError(c, http.StatusBadRequest, "invalid_data",
			"Invalid input data", err)
	case errors.Is(err, entity.ErrProcessingFailed):
		h.respondError(c, http.StatusInternalServerError, "processing_failed",
			"Image processing failed", nil)
	default:
		h.respondError(c, http.StatusInternalServerError, "internal_error",
			"Internal server error occurred", err)
	}
}
