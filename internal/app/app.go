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
	handler "img-processor/internal/transport/http"
	kafkat "img-processor/internal/transport/kafka"
	"img-processor/pkg/storage"

	"github.com/wb-go/wbf/logger"
	"golang.org/x/sync/errgroup"
)

func Run(ctx context.Context, cfg *config.Config, log logger.Logger) error {
	const op = "app.Run"
	var (
		store        storage.Provider
		kafkaAdapter *kafkat.KafkaAdapter
		err          error
	)

	store, err = initStorage(&cfg.Storage)
	if err != nil {
		return fmt.Errorf("%s: init storage: %w", op, err)
	}
	log.LogAttrs(ctx, logger.InfoLevel, "storage initialized",
		logger.String("type", cfg.Storage.Type),
	)

	repo := repository.NewImageRepository(store, log)

	imgProc, err := processor.NewImageProcessor(log,
		processor.MaxWidth(cfg.Service.MaxWidth),
		processor.ThumbSize(cfg.Service.ThumbSize),
		processor.JPEGQuality(cfg.Service.JPEGQuality),
	)
	if err != nil {
		return fmt.Errorf("%s: init image processor: %w", op, err)
	}

	svc := service.NewProcessorService(
		repo, imgProc, nil, log,
		service.MaxFileSizeMB(cfg.Service.MaxFileSizeMB),
		service.EnableWatermark(cfg.Service.EnableWatermark),
		service.WatermarkPath(cfg.Service.WatermarkPath),
	)

	if len(cfg.Kafka.Brokers) > 0 {
		kafkaAdapter, err = kafkat.NewKafkaAdapter(cfg.Kafka, log)
		if err != nil {
			return fmt.Errorf("%s: init kafka: %w", op, err)
		}
		kafkaAdapter.SetProcessorService(svc)
		svc.SetKafka(kafkaAdapter)
		defer func() {
			if err = kafkaAdapter.Close(); err != nil {
				log.LogAttrs(ctx, logger.WarnLevel, "kafka close error", logger.Any("error", err))
			}
		}()
		log.LogAttrs(ctx, logger.InfoLevel, "kafka initialized",
			logger.String("topic", cfg.Kafka.Topic),
			logger.String("group_id", cfg.Kafka.GroupID),
		)
	}

	h := handler.NewImageHandler(svc, log)

	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		return startHTTPServer(ctx, h, &cfg.HTTP, log)
	})

	if kafkaAdapter != nil {
		eg.Go(func() error {
			log.LogAttrs(ctx, logger.InfoLevel, "starting kafka consumer")
			kafkaAdapter.Start(ctx)
			<-ctx.Done()
			return nil
		})
	}

	eg.Go(func() error {
		return runCleanupLoop(ctx, log, repo, svc, cfg.Service.CleanupInterval, cfg.Service.CleanupMaxAge)
	})

	if err = eg.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("%s: %w", op, err)
	}

	log.LogAttrs(ctx, logger.InfoLevel, "application stopped gracefully")
	return nil
}

func startHTTPServer(
	ctx context.Context,
	h *handler.ImageHandler,
	cfg *config.HTTP,
	log logger.Logger,
) error {
	const op = "app.startHTTPServer"
	server := handler.NewHTTPServer(h, cfg, log)

	if err := server.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

func initStorage(cfg *config.Storage) (storage.Provider, error) {
	const op = "app.initStorage"

	switch cfg.Type {
	case "minio":
		store, err := storage.NewMinIO(*cfg)
		if err != nil {
			return nil, fmt.Errorf("%s: init minio: %w", op, err)
		}
		return store, nil
	case "local", "":
		store, err := storage.NewLocal(*cfg)
		if err != nil {
			return nil, fmt.Errorf("%s: init local: %w", op, err)
		}
		return store, nil
	default:
		return nil, fmt.Errorf("%s: unsupported storage type: %q", op, cfg.Type)
	}
}

func runCleanupLoop(
	ctx context.Context,
	log logger.Logger,
	repo *repository.ImageRepository,
	svc *service.ProcessorService,
	interval time.Duration,
	maxAge time.Duration,
) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.LogAttrs(ctx, logger.InfoLevel, "cleanup loop started",
		logger.Duration("interval", interval),
		logger.Duration("max_age", maxAge),
	)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			now := time.Now()
			cutoffTime := now.Add(-maxAge)

			if deleted, err := repo.Cleanup(ctx, maxAge); err != nil {
				log.LogAttrs(ctx, logger.WarnLevel, "cleanup iteration failed (disk)", logger.Any("error", err))
			} else if deleted > 0 {
				log.LogAttrs(ctx, logger.InfoLevel, "cleanup tick (files)", logger.Int("deleted", deleted))
			}

			if svc != nil {
				memDeleted := svc.CleanupMemoryTasks(ctx, cutoffTime)
				if memDeleted > 0 {
					log.LogAttrs(ctx, logger.InfoLevel, "cleanup tick (memory)", logger.Int("deleted", memDeleted))
				}
			}
		}
	}
}
