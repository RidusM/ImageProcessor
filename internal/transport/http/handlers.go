// nolint: revive,staticcheck
package handler

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"img-processor/internal/entity"
	"img-processor/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// @Summary Upload an image
// @Description Uploads an image for processing (resize, thumbnail, watermark)
// @Image Tags
// @Accept multipart/form-data
// @Generate JSON
// @Param image formData file true "Image file"
// Options @Param formData string false "JSON with processing options"
// @Success 202 {object} UploadResponse "Image accepted for processing"
// @Failure 400 {object} ErrorResponse "Validation error"
// @Failure 413 {object} ErrorResponse "File too large"
// @Failure 500 {object} ErrorResponse "Internal error"
// @Router /upload [post]
func (h *ImageHandler) UploadImage(c *gin.Context) {
	ctx := c.Request.Context()

	var req UploadRequest
	if err := c.ShouldBind(&req); err != nil {
		h.respondError(c, http.StatusBadRequest, "bind_error", "Invalid request format", err)
		return
	}

	serviceReq := service.UploadRequest{
		File:    req.File,
		Options: &entity.ProcessingOptions{},
	}

	if req.Options != nil {
		serviceReq.Options = &entity.ProcessingOptions{
			ResizeWidth:      req.Options.ResizeWidth,
			ResizeHeight:     req.Options.ResizeHeight,
			ThumbnailSize:    req.Options.ThumbnailSize,
			AddWatermark:     req.Options.AddWatermark,
			ConvertTo:        req.Options.ConvertTo,
			Quality:          req.Options.Quality,
			PreserveMetadata: req.Options.PreserveMetadata,
		}
	}

	image, err := h.svc.Upload(ctx, serviceReq)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}

	response := UploadResponse{
		ID:       image.ID,
		Status:   image.Status,
		Filename: image.Filename,
		Size:     image.Size,
		Message:  image.Message,
	}

	c.Header("Location", fmt.Sprintf("/image/%s", image.ID))
	h.respondJSON(c, http.StatusAccepted, response)
}

// @Summary Get an image or status
// @Description Returns the processed image or task status
// @Tags Image
// @Produce json
// @Produce image/jpeg
// @Produce image/png
// @Param id path string true "Image ID"
// @Param version query string false "Version: processed|original|thumb" default(processed)
// @Success 200 {file} binary "Processed image"
// @Success 202 {object} ImageStatusResponse "Image is still being processed"
// @Failure 400 {object} ErrorResponse "Invalid ID"
// @Failure 404 {object} ErrorResponse "Image not found"
// @Failure 500 {object} ErrorResponse "Internal error"
// @Router /image/{id} [get]
func (h *ImageHandler) GetImage(c *gin.Context) {
	ctx := c.Request.Context()

	idStr := c.Param("id")
	imageID, err := uuid.Parse(idStr)
	if err != nil {
		h.respondError(c, http.StatusBadRequest, "invalid_id", "Invalid notification ID format", err)
		return
	}

	version := c.DefaultQuery("version", "processed")
	reader, size, ext, err := h.svc.GetImage(ctx, imageID, version)
	if err != nil {
		if errors.Is(err, entity.ErrImageNotFound) {
			task, _ := h.svc.GetStatus(ctx, imageID)
			if task != nil && !task.Status.IsTerminal() {
				h.respondJSON(c, http.StatusAccepted, ImageStatusResponse{
					ID:        task.ImageID,
					Status:    string(task.Status),
					Progress:  task.Progress,
					CreatedAt: task.CreatedAt,
				})
				return
			}
		}
		h.handleServiceError(c, err)
		return
	}
	defer reader.Close()

	c.Header("Content-Type", contentTypeForExt(ext))
	c.Header("Content-Length", strconv.FormatInt(size, 10))
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("X-Image-ID", imageID.String())
	c.Header("X-Image-Version", version)

	c.Stream(func(w io.Writer) bool {
		_, err = io.Copy(w, reader)
		return err == nil
	})
}

// @Summary Get the processing status
// @Description Returns the current status of the image processing task
// @Tags Image
// @Accept json
// @Produce json
// @Param id path string true "Image ID"
// @Success 200 {object} ImageStatusResponse "Processing complete"
// @Success 202 {object} ImageStatusResponse "Processing in progress"
// @Failure 400 {object} ErrorResponse "Invalid ID"
// @Failure 404 {object} ErrorResponse "Task not found"
// @Failure 500 {object} ErrorResponse "Internal error"
// @Router /image/{id}/status [get]
func (h *ImageHandler) GetStatus(c *gin.Context) {
	ctx := c.Request.Context()

	idStr := c.Param("id")
	imageID, err := uuid.Parse(idStr)
	if err != nil {
		h.respondError(c, http.StatusBadRequest, "invalid_id", "Invalid notification ID format", err)
		return
	}

	task, err := h.svc.GetStatus(ctx, imageID)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}

	response := ImageStatusResponse{
		ID:        task.ImageID,
		Status:    string(task.Status),
		Progress:  task.Progress,
		CreatedAt: task.CreatedAt,
	}
	if task.Status == entity.StatusError {
		errMsg := task.ErrorMessage
		response.Error = &errMsg
	}
	if task.CompletedAt != nil {
		response.UpdatedAt = task.CompletedAt
	}

	statusCode := http.StatusOK
	if !task.Status.IsTerminal() {
		statusCode = http.StatusAccepted
	}
	h.respondJSON(c, statusCode, response)
}

// @Summary Delete image
// @Description Deletes the image and all its versions (original, processed, thumbnail)
// @Tags Image
// @Accept json
// @Produce json
// @Param id path string true "Image ID"
// @Success 204 "Image deleted"
// @Failure 400 {object} ErrorResponse "Invalid ID"
// @Failure 500 {object} ErrorResponse "Internal error"
// @Router /image/{id} [delete]
func (h *ImageHandler) DeleteImage(c *gin.Context) {
	ctx := c.Request.Context()

	idStr := c.Param("id")
	imageID, err := uuid.Parse(idStr)
	if err != nil {
		h.respondError(c, http.StatusBadRequest, "invalid_id", "Invalid notification ID format", err)
		return
	}

	if err = h.svc.Delete(ctx, imageID); err != nil {
		h.handleServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// @Summary Health check endpoint
// @Description Return service status and current timestamp. No authentication required.
// @Tags System
// @Produce json
// @Success 200 {object} HealthResponse "Service is healthy"
// @Router /health [get]
func (h *ImageHandler) Health(c *gin.Context) {
	response := HealthResponse{
		Status: "ok",
		Time:   time.Now(),
	}
	h.respondJSON(c, http.StatusOK, response)
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

func contentTypeForExt(ext string) string {
	switch ext {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}
