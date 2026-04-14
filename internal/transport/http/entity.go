package handlers

import "time"

type UploadResponse struct {
	ID       string `json:"id"       example:"a1b2c3d4e5f6"`
	Status   string `json:"status"   example:"pending"`
	Filename string `json:"filename" example:"photo.jpg"`
	Size     int64  `json:"size"     example:"2048576"`
	Message  string `json:"message"  example:"Image uploaded successfully"`
}

type ImageStatusResponse struct {
	ID        string     `json:"id"                   example:"a1b2c3d4e5f6"`
	Filename  string     `json:"filename,omitempty"   example:"photo.jpg"`
	Status    string     `json:"status"               example:"processing"`
	Progress  int        `json:"progress"             example:"45"`
	Error     *string    `json:"error,omitempty"      example:"resize failed"`
	CreatedAt time.Time  `json:"created_at"           example:"2024-01-15T10:30:00Z"`
	UpdatedAt *time.Time `json:"updated_at,omitempty" example:"2024-01-15T10:30:05Z"`
}

type ErrorResponse struct {
	Error   string `json:"error"             example:"image not found"`
	Code    string `json:"code,omitempty"    example:"not_found"`
	Details string `json:"details,omitempty" example:"image with id abc123 does not exist"`
}

type HealthResponse struct {
	Status string `json:"status" example:"ok"`
	Time   string `json:"time"   example:"2024-01-15T10:30:00Z"`
}
