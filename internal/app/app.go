package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"img-processor/internal/config"
	"img-processor/internal/processor"
	"img-processor/internal/repository"
	"img-processor/internal/service"
	handlers "img-processor/internal/transport/http"
	"img-processor/internal/worker"

	"github.com/wb-go/wbf/logger"
	"golang.org/x/sync/errgroup"
)

const _workerTaskTimeout = 5 * time.Minute

func Run(ctx context.Context, cfg *config.Config, log logger.Logger) error {
	const op = "app.Run"
	log.LogAttrs(ctx, logger.InfoLevel, "application starting",
		logger.String("name", cfg.App.Name),
		logger.String("version", cfg.App.Version),
		logger.String("env", cfg.Env),
	)

	// 1. Repository
	repo, err := repository.NewImageRepository(cfg.Storage, log)
	if err != nil {
		return fmt.Errorf("%s: init repository: %w", op, err)
	}

	// 2. Processor
	proc, err := processor.NewImageProcessor(log,
		processor.MaxWidth(cfg.Service.MaxWidth),
		processor.ThumbSize(cfg.Service.ThumbSize),
		processor.JPEGQuality(cfg.Service.JPEGQuality),
	)
	if err != nil {
		return fmt.Errorf("%s: init processor: %w", op, err)
	}

	// 4. Service
	svc, err := service.NewProcessorService(repo, proc, nil, log,
		service.MaxFileSizeMB(cfg.Service.MaxFileSizeMB),
		service.EnableWatermark(cfg.Service.EnableWatermark),
		service.WatermarkPath(cfg.Service.WatermarkPath),
	)
	if err != nil {
		return fmt.Errorf("%s: init service: %w", op, err)
	}

	pool, err := worker.NewPool(svc, log,
		worker.PoolSize(cfg.Service.WorkerCount),
		worker.QueueSize(cfg.Service.QueueSize),
		worker.TaskTimeout(_workerTaskTimeout),
	)
	if err != nil {
		return fmt.Errorf("%s: init worker pool: %w", op, err)
	}

	// 5. Handlers & Server
	handler := handlers.NewImageHandler(svc, log)
	server := handlers.NewHTTPServer(handler, &cfg.HTTP, log)

	eg, ctx := errgroup.WithContext(ctx)

	// Start worker pool
	pool.Start(ctx)

	// Start cleanup loop (local only)
	if cfg.Storage.Type == "local" {
		eg.Go(func() error {
			return runCleanupLoop(ctx, log, repo, 1*time.Hour)
		})
	}

	// Start HTTP server
	eg.Go(func() error {
		if err = server.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	})

	// Wait for shutdown
	if err = eg.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("%s: %w", op, err)
	}

	pool.Stop(ctx)
	log.LogAttrs(ctx, logger.InfoLevel, "application stopped gracefully")
	return nil
}

func runCleanupLoop(
	ctx context.Context,
	log logger.Logger,
	repo repository.ImageRepository,
	interval time.Duration,
) error {
	const op = "app.runCleanupLoop"

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: context closed: %w", op, ctx.Err())
		case <-ticker.C:
			if cleaner, ok := repo.(interface{ Cleanup(context.Context) error }); ok {
				if err := cleaner.Cleanup(ctx); err != nil {
					log.LogAttrs(ctx, logger.WarnLevel, "cleanup failed", logger.Any("error", err))
				}
			}
		}
	}
}
