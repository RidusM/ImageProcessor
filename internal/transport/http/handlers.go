// nolint: revive,staticcheck
package handlers

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
)

// UploadImage handles POST /upload.
// @Summary Загрузить изображение
// @Description Загружает изображение на обработку (ресайз, миниатюра, водяной знак)
// @Tags Image
// @Accept multipart/form-data
// @Produce json
// @Param image formData file true "Файл изображения"
// @Param options formData string false "JSON с опциями обработки"
// @Success 202 {object} UploadResponse "Изображение принято в обработку"
// @Failure 400 {object} ErrorResponse "Ошибка валидации"
// @Failure 413 {object} ErrorResponse "Файл слишком большой"
// @Failure 500 {object} ErrorResponse "Внутренняя ошибка"
// @Router /upload [post]
func (h *ImageHandler) UploadImage(c *gin.Context) {
	const op = "handlers.UploadImage"
	ctx := c.Request.Context()

	if err := c.Request.ParseMultipartForm(_maxRequestBodySize); err != nil {
		h.respondError(c, http.StatusBadRequest, "parse_error", "Failed to parse multipart form", err)
		return
	}

	header, err := c.FormFile("image")
	if err != nil {
		if errors.Is(err, http.ErrMissingFile) {
			h.respondError(c, http.StatusBadRequest, "missing_file", "Field 'image' is required", nil)
			return
		}
		h.respondError(c, http.StatusBadRequest, "file_error", "Failed to get file", err)
		return
	}

	resp, err := h.svc.Upload(ctx, service.UploadRequest{
		File:    header,
		Options: nil,
	})
	if err != nil {
		h.handleServiceError(c, op, err)
		return
	}

	response := UploadResponse{
		ID:       resp.ID,
		Status:   resp.Status,
		Filename: resp.Filename,
		Size:     resp.Size,
		Message:  resp.Message,
	}

	c.Header("Location", fmt.Sprintf("/image/%s", resp.ID))
	h.respondJSON(c, http.StatusAccepted, response)
}

// GetImage handles GET /image/:id.
// @Summary Получить изображение или статус
// @Description Возвращает обработанное изображение или статус задачи
// @Tags Image
// @Produce json
// @Produce image/jpeg
// @Produce image/png
// @Param id path string true "ID изображения"
// @Param version query string false "Версия: processed|original|thumb" default(processed)
// @Success 200 {file} binary "Обработанное изображение"
// @Success 202 {object} ImageStatusResponse "Изображение ещё обрабатывается"
// @Failure 400 {object} ErrorResponse "Неверный ID"
// @Failure 404 {object} ErrorResponse "Изображение не найдено"
// @Failure 500 {object} ErrorResponse "Внутренняя ошибка"
// @Router /image/{id} [get]
func (h *ImageHandler) GetImage(c *gin.Context) {
	const op = "handlers.GetImage"
	ctx := c.Request.Context()
	imageID := c.Param("id")
	if imageID == "" {
		h.respondError(c, http.StatusBadRequest, "missing_id", "Image ID is required", nil)
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
		h.handleServiceError(c, op, err)
		return
	}
	defer reader.Close()

	c.Header("Content-Type", contentTypeForExt(ext))
	c.Header("Content-Length", strconv.FormatInt(size, 10))
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("X-Image-ID", imageID)
	c.Header("X-Image-Version", version)

	c.Stream(func(w io.Writer) bool {
		_, err = io.Copy(w, reader)
		return err == nil
	})
}

// GetStatus handles GET /image/:id/status.
// @Summary Получить статус обработки
// @Description Возвращает текущий статус задачи обработки изображения
// @Tags Image
// @Accept json
// @Produce json
// @Param id path string true "ID изображения"
// @Success 200 {object} ImageStatusResponse "Обработка завершена"
// @Success 202 {object} ImageStatusResponse "Обработка в процессе"
// @Failure 400 {object} ErrorResponse "Неверный ID"
// @Failure 404 {object} ErrorResponse "Задача не найдена"
// @Failure 500 {object} ErrorResponse "Внутренняя ошибка"
// @Router /image/{id}/status [get]
func (h *ImageHandler) GetStatus(c *gin.Context) {
	const op = "handlers.GetStatus"
	ctx := c.Request.Context()
	imageID := c.Param("id")
	if imageID == "" {
		h.respondError(c, http.StatusBadRequest, "missing_id", "Image ID is required", nil)
		return
	}

	task, err := h.svc.GetStatus(ctx, imageID)
	if err != nil {
		h.handleServiceError(c, op, err)
		return
	}

	response := ImageStatusResponse{
		ID:        task.ImageID,
		Status:    string(task.Status),
		Progress:  task.Progress,
		CreatedAt: task.CreatedAt,
	}
	if task.Status == entity.TaskStatusError {
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

// DeleteImage handles DELETE /image/:id.
// @Summary Удалить изображение
// @Description Удаляет изображение и все его версии (оригинал, обработанное, миниатюру)
// @Tags Image
// @Accept json
// @Produce json
// @Param id path string true "ID изображения"
// @Success 204 "Изображение удалено"
// @Failure 400 {object} ErrorResponse "Неверный ID"
// @Failure 500 {object} ErrorResponse "Внутренняя ошибка"
// @Router /image/{id} [delete]
func (h *ImageHandler) DeleteImage(c *gin.Context) {
	const op = "handlers.DeleteImage"
	ctx := c.Request.Context()
	imageID := c.Param("id")
	if imageID == "" {
		h.respondError(c, http.StatusBadRequest, "missing_id", "Image ID is required", nil)
		return
	}

	if err := h.svc.Delete(ctx, imageID); err != nil {
		h.handleServiceError(c, op, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Health handles GET /health.
// @Summary Health check
// @Description Проверка доступности сервиса
// @Tags System
// @Produce json
// @Success 200 {object} HealthResponse "Сервис доступен"
// @Router /health [get]
func (h *ImageHandler) Health(c *gin.Context) {
	h.respondJSON(c, http.StatusOK, HealthResponse{
		Status: "ok",
		Time:   time.Now().Format(time.RFC3339),
	})
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
