//nolint:revive,staticcheck
package handler

import (
	"mime/multipart"
	"time"

	"github.com/google/uuid"
)

// swagger:model UploadRequest
type UploadRequest struct {
	File     *multipart.FileHeader `form:"file"      binding:"required"`
	ClientID uuid.UUID             `form:"client_id"                    validate:"required,uuid"`
	Options  *ProcessingOptions    `form:"options"`
}

type ProcessingOptions struct {
	ResizeWidth      int    `json:"resize_width"`
	ResizeHeight     int    `json:"resize_height"`
	ThumbnailSize    int    `json:"thumbnail_size"`
	AddWatermark     bool   `json:"add_watermark"`
	ConvertTo        string `json:"convert_to"`
	Quality          int    `json:"quality"`
	PreserveMetadata bool   `json:"preserve_metadata"`
}

// swagger:model UploadResponse
type UploadResponse struct {
	ID       uuid.UUID `json:"id"       example:"550e8400-e29b-41d4-a716-446655440002"`
	Status   string    `json:"status"   example:"pending"`
	Filename string    `json:"filename" example:"photo.jpg"`
	Size     int64     `json:"size"     example:"2048576"`
	Message  string    `json:"message"  example:"Image uploaded successfully"`
}

// swagger:model ImageStatusResponse
type ImageStatusResponse struct {
	ID        uuid.UUID  `json:"id"                   example:"550e8400-e29b-41d4-a716-446655440002"`
	Filename  string     `json:"filename,omitempty"   example:"photo.jpg"`
	Status    string     `json:"status"               example:"processing"`
	Progress  int        `json:"progress"             example:"45"`
	Error     *string    `json:"error,omitempty"      example:"resize failed"`
	CreatedAt time.Time  `json:"created_at"           example:"2024-01-15T10:30:00Z"`
	UpdatedAt *time.Time `json:"updated_at,omitempty" example:"2024-01-15T10:30:05Z"`
}

// swagger:model ErrorResponse
type ErrorResponse struct {
	Error   string `json:"error"             example:"image not found"`
	Code    string `json:"code,omitempty"    example:"not_found"`
	Details string `json:"details,omitempty" example:"image with name photo1 does not exist"`
}

// swagger:model SuccessResponse
type SuccessResponse struct {
	Message string `json:"message" example:"Operation completed successfully"`
}

// swagger:model HealthResponse
type HealthResponse struct {
	Status string    `json:"status" example:"ok"`
	Time   time.Time `json:"time"   example:"2026-05-08T06:04:15Z"`
}
