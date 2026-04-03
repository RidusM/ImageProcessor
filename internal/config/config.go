package config

import (
	"time"
)

type (
	Config struct {
		App     App     `env-prefix:"APP_"`
		Service Service `env-prefix:"SERVICE_"`
		Storage Storage `env-prefix:"STORAGE_"`
		HTTP    HTTP    `env-prefix:"HTTP_"`
		Logger  Logger  `env-prefix:"LOGGER_"`
		Env     string  `env:"ENV" env-default:"local" validate:"oneof=local dev staging prod"`
	}

	App struct {
		Name    string `env:"NAME"    env-default:"img-processor" validate:"required"`
		Version string `env:"VERSION" env-default:"1.0.0"         validate:"required"`
	}

	Service struct {
		MaxFileSizeMB int    `env:"MAX_FILE_SIZE_MB" env-default:"32" validate:"min=1,max=100"`
		WatermarkPath string `env:"WATERMARK_PATH"   env-default:"./assets/watermark.png"`
		WorkerCount   int    `env:"WORKER_COUNT"     env-default:"4"  validate:"min=1,max=16"`
		QueueSize     int    `env:"QUEUE_SIZE"       env-default:"100" validate:"min=10,max=1000"`

		MaxWidth      int `env:"MAX_WIDTH"       env-default:"1920" validate:"min=100,max=4096"`
		ThumbSize     int `env:"THUMB_SIZE"      env-default:"300"  validate:"min=50,max=1000"`
		JPEGQuality   int `env:"JPEG_QUALITY"    env-default:"90"   validate:"min=1,max=100"`
		EnableWatermark bool `env:"ENABLE_WATERMARK" env-default:"true"`
	}

	Storage struct {
		Type string `env:"TYPE" env-default:"local" validate:"oneof=local minio"`

		Path string `env:"PATH" env-default:"./storage"`

		MinIOEndpoint  string `env:"MINIO_ENDPOINT"`
		MinIOAccessKey string `env:"MINIO_ACCESS_KEY"`
		MinIOSecretKey string `env:"MINIO_SECRET_KEY"`
		MinIOBucket    string `env:"MINIO_BUCKET" env-default:"img-processor"`
		MinIOUseSSL    bool   `env:"MINIO_USE_SSL" env-default:"false"`
		MinIORegion    string `env:"MINIO_REGION" env-default:"us-east-1"`
	}

	HTTP struct {
		Host              string        `env:"HOST"                env-default:"0.0.0.0" validate:"required"`
		Port              string        `env:"PORT"                env-default:"8080"    validate:"required"`
		ReadTimeout       time.Duration `env:"READ_TIMEOUT"        env-default:"10s"     validate:"gte=1s,lte=30s"`
		WriteTimeout      time.Duration `env:"WRITE_TIMEOUT"       env-default:"30s"     validate:"gte=1s,lte=60s"`
		IdleTimeout       time.Duration `env:"IDLE_TIMEOUT"        env-default:"60s"     validate:"gte=1s,lte=300s"`
		ShutdownTimeout   time.Duration `env:"SHUTDOWN_TIMEOUT"    env-default:"10s"     validate:"gte=1s,lte=30s"`
		ReadHeaderTimeout time.Duration `env:"READ_HEADER_TIMEOUT" env-default:"5s"      validate:"gte=1s,lte=30s"`
		MaxHeaderBytes    int           `env:"MAX_HEADER_BYTES"    env-default:"1048576" validate:"required,gte=1024,lte=10485760"`
	}

	Logger struct {
		Level      string `env:"LEVEL"       env-default:"info"                        validate:"oneof=debug info warn error"`
		Filename   string `env:"FILENAME"    env-default:"./logs/img-processor.log"`
		MaxSize    int    `env:"MAX_SIZE"    env-default:"100"                         validate:"min=1,max=1000"`
		MaxBackups int    `env:"MAX_BACKUPS" env-default:"3"                           validate:"min=0,max=20"`
		MaxAge     int    `env:"MAX_AGE"     env-default:"28"                          validate:"min=1,max=365"`
		Compress   bool   `env:"COMPRESS"    env-default:"true"`
	}
)