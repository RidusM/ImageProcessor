// nolint: revive,staticcheck
package httpt

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"img-processor/internal/entity"
	"img-processor/internal/service"

	"github.com/gin-gonic/gin"
)

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
	const op = "transport.httpt.ImageHandler.UploadImage"
	ctx := c.Request.Context()

	// Parse multipart form
	if err := c.Request.ParseMultipartForm(_maxRequestBodySize); err != nil {
		h.respondError(c, http.StatusBadRequest, "parse_error", "Failed to parse multipart form", err)
		return
	}

	// Get file
	file, header, err := c.FormFile("image")
	if err != nil {
		if err == http.ErrMissingFile {
			h.respondError(c, http.StatusBadRequest, "missing_file", "Field 'image' is required", nil)
			return
		}
		h.respondError(c, http.StatusBadRequest, "file_error", "Failed to get file", err)
		return
	}
	defer file.Close()

	// Parse options (optional)
	var opts *entity.ProcessingOptions
	if optsStr := c.PostForm("options"); optsStr != "" {
		// Можно добавить парсинг JSON при необходимости
		_ = optsStr
	}

	// Call service
	serviceReq := service.UploadRequest{
		File:    header,
		Options: opts,
	}

	resp, err := h.svc.Upload(ctx, serviceReq)
	if err != nil {
		h.handleServiceError(c, op, err)
		return
	}

	response := UploadResponse{
		ID:       resp.ID,
		Status:   resp.Status,
		Filename: resp.Filename,
		Size:     header.Size,
		Message:  "Image uploaded successfully",
	}

	c.Header("Location", fmt.Sprintf("/image/%s", resp.ID))
	h.respondJSON(c, http.StatusAccepted, response)
}

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
	const op = "transport.httpt.ImageHandler.GetImage"
	ctx := c.Request.Context()

	imageID := c.Param("id")
	if imageID == "" {
		h.respondError(c, http.StatusBadRequest, "missing_id", "Image ID is required", nil)
		return
	}

	version := c.DefaultQuery("version", "processed")

	reader, size, ext, err := h.svc.GetImage(ctx, imageID, version)
	if err != nil {
		if err == entity.ErrImageNotFound {
			// Проверяем, не в процессе ли обработка
			task, _ := h.svc.GetStatus(ctx, imageID)
			if task != nil && !task.Status.IsTerminal() {
				response := ImageStatusResponse{
					ID:        task.ImageID,
					Status:    string(task.Status),
					Progress:  task.Progress,
					CreatedAt: task.CreatedAt,
				}
				h.respondJSON(c, http.StatusAccepted, response)
				return
			}
			h.respondError(c, http.StatusNotFound, "not_found", "Image not found", err)
			return
		}
		h.handleServiceError(c, op, err)
		return
	}
	defer reader.Close()

	// Set image headers
	contentType := contentTypeForExt(ext)
	c.Header("Content-Type", contentType)
	c.Header("Content-Length", fmt.Sprintf("%d", size))
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("X-Image-ID", imageID)
	c.Header("X-Image-Version", version)

	// Stream file
	c.Stream(func(w io.Writer) bool {
		_, err := io.Copy(w, reader)
		return err == nil
	})
}

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
	const op = "transport.httpt.ImageHandler.GetStatus"
	ctx := c.Request.Context()

	imageID := c.Param("id")
	if imageID == "" {
		h.respondError(c, http.StatusBadRequest, "missing_id", "Image ID is required", nil)
		return
	}

	task, err := h.svc.GetStatus(ctx, imageID)
	if err != nil {
		if err == entity.ErrImageNotFound {
			h.respondError(c, http.StatusNotFound, "not_found", "Image not found", err)
			return
		}
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
	const op = "transport.httpt.ImageHandler.DeleteImage"
	ctx := c.Request.Context()

	imageID := c.Param("id")
	if imageID == "" {
		h.respondError(c, http.StatusBadRequest, "missing_id", "Image ID is required", nil)
		return
	}

	if err := h.svc.Delete(ctx, imageID); err != nil {
		if err == entity.ErrImageNotFound {
			// Идемпотентность: удаление несуществующего — успех
			c.Status(http.StatusNoContent)
			return
		}
		h.handleServiceError(c, op, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// @Summary Health check
// @Description Проверка доступности сервиса
// @Tags System
// @Produce json
// @Success 200 {object} HealthResponse "Сервис доступен"
// @Router /health [get]
func (h *ImageHandler) Health(c *gin.Context) {
	response := HealthResponse{
		Status: "ok",
		Time:   time.Now().Format(time.RFC3339),
	}
	h.respondJSON(c, http.StatusOK, response)
}

// @Summary Ready check
// @Description Проверка готовности сервиса (зависимости доступны)
// @Tags System
// @Produce json
// @Success 200 {object} HealthResponse "Сервис готов"
// @Router /ready [get]
func (h *ImageHandler) Ready(c *gin.Context) {
	// Здесь можно добавить проверки: хранилище, воркеры и т.д.
	response := HealthResponse{
		Status: "ready",
		Time:   time.Now().Format(time.RFC3339),
	}
	h.respondJSON(c, http.StatusOK, response)
}

// contentTypeForExt возвращает MIME-тип для расширения
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