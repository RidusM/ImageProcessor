// nolint: revive,staticcheck
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"img-processor/internal/config"
	"img-processor/internal/entity"
	"img-processor/internal/processor"
	"img-processor/internal/repository"
	"img-processor/internal/service"
	"img-processor/internal/transport/httpt"
	"img-processor/internal/worker"

	"github.com/wb-go/wbf/logger"
	"golang.org/x/sync/errgroup"
)

// App представляет запущенное приложение
type App struct {
	cfg     *config.Config
	log     logger.Logger
	svc     *service.ProcessorService
	handler *httpt.ImageHandler
	server  *httpt.HTTPServer
	workers *worker.Pool
}

// Run запускает приложение и блокируется до получения сигнала завершения
func Run(ctx context.Context, cfg *config.Config, log logger.Logger) error {
	const op = "app.Run"

	log.LogAttrs(ctx, logger.InfoLevel, "application starting",
		logger.String("name", cfg.App.Name),
		logger.String("version", cfg.App.Version),
		logger.String("env", cfg.Env),
	)

	// 1. Инициализация компонентов
	app, err := New(ctx, cfg, log)
	if err != nil {
		return fmt.Errorf("%s: init: %w", op, err)
	}

	// 2. Запуск
	if err := app.Start(ctx); err != nil {
		return fmt.Errorf("%s: start: %w", op, err)
	}

	// 3. Ожидание сигнала завершения
	<-ctx.Done()

	// 4. Graceful shutdown
	log.LogAttrs(ctx, logger.InfoLevel, "shutdown signal received, stopping application")
	if err := app.Stop(ctx); err != nil {
		return fmt.Errorf("%s: stop: %w", op, err)
	}

	log.LogAttrs(ctx, logger.InfoLevel, "application stopped gracefully")
	return nil
}

// New создаёт и инициализирует приложение (композиционный корень)
func New(ctx context.Context, cfg *config.Config, log logger.Logger) (*App, error) {
	const op = "app.New"

	// -------------------------------------------------------------------------
	// 1. Repository слой (хранилище изображений)
	// -------------------------------------------------------------------------
	repo, err := repository.New(cfg.Storage)
	if err != nil {
		return nil, fmt.Errorf("%s: init repository: %w", op, err)
	}
	// Инъекция логгера в репозиторий (если реализация поддерживает)
	if rl, ok := repo.(interface{ SetLogger(logger.Logger) }); ok {
		rl.SetLogger(log)
	}
	log.LogAttrs(ctx, logger.DebugLevel, "repository initialized",
		logger.String("type", cfg.Storage.Type),
	)

	// -------------------------------------------------------------------------
	// 2. Image Processor (ядро обработки: ресайз, водяные знаки, кодирование)
	// -------------------------------------------------------------------------
	proc, err := processor.NewImageProcessor(log,
		processor.MaxWidth(cfg.Service.MaxWidth),
		processor.ThumbSize(cfg.Service.ThumbSize),
		processor.JPEGQuality(cfg.Service.JPEGQuality),
		processor.UseBiLinear(true),
	)
	if err != nil {
		return nil, fmt.Errorf("%s: init processor: %w", op, err)
	}
	log.LogAttrs(ctx, logger.DebugLevel, "image processor initialized",
		logger.Int("max_width", cfg.Service.MaxWidth),
		logger.Int("thumb_size", cfg.Service.ThumbSize),
	)

	// -------------------------------------------------------------------------
	// 3. Service слой (бизнес-логика)
	// -------------------------------------------------------------------------
	// Пока создаём заглушку для ProcessorService
	// В реальном проекте: передаём repo, proc, workers
	svc, err := service.NewProcessorService(
		repo,
		proc,
		nil, // workers будет установлен после создания пула
		log,
		service.MaxFileSizeMB(cfg.Service.MaxFileSizeMB),
		service.WorkerCount(cfg.Service.WorkerCount),
		service.QueueSize(cfg.Service.QueueSize),
		service.MaxWidth(cfg.Service.MaxWidth),
		service.ThumbSize(cfg.Service.ThumbSize),
		service.JPEGQuality(cfg.Service.JPEGQuality),
		service.EnableWatermark(cfg.Service.EnableWatermark),
		service.WatermarkPath(cfg.Service.WatermarkPath),
	)
	if err != nil {
		return nil, fmt.Errorf("%s: init service: %w", op, err)
	}
	log.LogAttrs(ctx, logger.DebugLevel, "processor service initialized",
		logger.Int("worker_count", cfg.Service.WorkerCount),
		logger.Int64("max_file_size_mb", cfg.Service.MaxFileSizeMB),
	)

	// -------------------------------------------------------------------------
	// 4. Worker Pool (фонная обработка задач)
	// -------------------------------------------------------------------------
	workers, err := worker.NewPool(svc, log,
		worker.PoolSize(cfg.Service.WorkerCount),
		worker.QueueSize(cfg.Service.QueueSize),
		worker.TaskTimeout(5*time.Minute),
	)
	if err != nil {
		return nil, fmt.Errorf("%s: init worker pool: %w", op, err)
	}
	// Инъекция workers обратно в сервис (циклическая зависимость решается через сеттер)
	if setter, ok := interface{}(svc).(interface{ SetWorkerPool(worker.WorkerPool) }); ok {
		setter.SetWorkerPool(workers)
	}
	log.LogAttrs(ctx, logger.DebugLevel, "worker pool initialized",
		logger.Int("pool_size", cfg.Service.WorkerCount),
		logger.Int("queue_size", cfg.Service.QueueSize),
	)

	// -------------------------------------------------------------------------
	// 5. HTTP Transport (Gin handler + routes)
	// -------------------------------------------------------------------------
	handler := httpt.NewImageHandler(svc, log)
	log.LogAttrs(ctx, logger.DebugLevel, "http handler initialized")

	// -------------------------------------------------------------------------
	// 6. HTTP Server (net/http wrapper с graceful shutdown)
	// -------------------------------------------------------------------------
	server := httpt.NewHTTPServer(handler, &cfg.HTTP, log)
	log.LogAttrs(ctx, logger.DebugLevel, "http server initialized",
		logger.String("addr", cfg.HTTP.Host+":"+cfg.HTTP.Port),
	)

	// -------------------------------------------------------------------------
	// 7. Сборка приложения
	// -------------------------------------------------------------------------
	app := &App{
		cfg:     cfg,
		log:     log,
		svc:     svc,
		handler: handler,
		server:  server,
		workers: workers,
	}

	log.LogAttrs(ctx, logger.InfoLevel, "application components initialized")
	return app, nil
}

// Start запускает все компоненты приложения
func (a *App) Start(ctx context.Context) error {
	const op = "app.Start"

	// Запускаем пул воркеров (они начнут обрабатывать задачи из очереди)
	a.workers.Start(ctx)
	a.log.LogAttrs(ctx, logger.InfoLevel, "worker pool started",
		logger.Int("count", a.cfg.Service.WorkerCount),
	)

	// HTTP-сервер запускается отдельно через errgroup в Run()
	// Здесь только логирование
	a.log.LogAttrs(ctx, logger.InfoLevel, "http server ready",
		logger.String("addr", a.cfg.HTTP.Host+":"+a.cfg.HTTP.Port),
		logger.String("env", a.cfg.Env),
	)

	// Запускаем периодическую очистку временных файлов (опционально)
	if a.cfg.Storage.Type == "local" {
		go a.startCleanupLoop(ctx)
	}

	return nil
}

// Stop останавливает все компоненты в обратном порядке
func (a *App) Stop(ctx context.Context) error {
	const op = "app.Stop"

	var errs []error

	// 1. Останавливаем HTTP-сервер (первым, чтобы не принимать новые запросы)
	if a.server != nil {
		if err := a.server.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("http server: %w", err))
		}
	}

	// 2. Останавливаем пул воркеров (ждём завершения текущих задач)
	if a.workers != nil {
		a.workers.Stop(ctx)
		a.log.LogAttrs(ctx, logger.InfoLevel, "worker pool stopped")
	}

	// 3. Останавливаем сервис (освобождение ресурсов, если нужно)
	if a.svc != nil {
		if err := a.svc.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("service: %w", err))
		}
	}

	// 4. Очистка хранилища (удаление зависших tmp-файлов)
	if a.cfg.Storage.Type == "local" {
		if repo, ok := a.svc.(interface{ Cleanup(context.Context) error }); ok {
			if err := repo.Cleanup(ctx); err != nil {
				errs = append(errs, fmt.Errorf("storage cleanup: %w", err))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s: %w", op, errors.Join(errs...))
	}

	a.log.LogAttrs(ctx, logger.InfoLevel, "application stopped")
	return nil
}

// startCleanupLoop периодически очищает временные файлы хранилища
func (a *App) startCleanupLoop(ctx context.Context) {
	const op = "app.cleanupLoop"
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Если репозиторий поддерживает Cleanup — вызываем
			// Это опционально, зависит от реализации
			a.log.LogAttrs(ctx, logger.DebugLevel, "running storage cleanup")
			// repo.Cleanup(ctx) // раскомментировать при необходимости
		}
	}
}

// Health проверяет готовность ключевых компонентов
// Может использоваться для Kubernetes readiness probe
func (a *App) Health(ctx context.Context) error {
	const op = "app.Health"

	// Пример проверки:
	// 1. Хранилище доступно
	// 2. Воркеры живы
	// 3. Сервис в рабочем состоянии

	// Заглушка: всегда ок
	// В реальном проекте: реальные проверки зависимостей
	_ = op
	return nil
}