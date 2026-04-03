package worker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"img-processor/internal/entity"
	"img-processor/internal/service"
	"github.com/wb-go/wbf/logger"
)

const (
	_defaultPoolSize  = 4
	_defaultQueueSize = 100
	_taskTimeout      = 10 * time.Minute
	_shutdownTimeout  = 30 * time.Second
)

type (
	// Task представляет задачу для воркера
	Task struct {
		ID          string
		ImageID     string
		Options     *entity.ProcessingOptions
		Status      *entity.TaskStatus
		Err         *error
		Done        chan struct{}
		SubmittedAt time.Time
	}

	// Pool пул воркеров для фоновой обработки
	Pool struct {
		service *service.ProcessorService
		log     logger.Logger

		// конфигурация
		poolSize  int
		queueSize int
		timeout   time.Duration

		// внутренние каналы и состояния
		tasks    chan *Task
		statuses sync.Map // map[string]*entity.Task
		wg       sync.WaitGroup
		stopOnce sync.Once
		stopCh   chan struct{}
	}
)

// NewPool создаёт новый пул воркеров
func NewPool(
	svc *service.ProcessorService,
	log logger.Logger,
	opts ...Option,
) (*Pool, error) {
	const op = "worker.pool.NewPool"

	p := &Pool{
		service:   svc,
		log:       log,
		poolSize:  _defaultPoolSize,
		queueSize: _defaultQueueSize,
		timeout:   _taskTimeout,
		tasks:     make(chan *Task, _defaultQueueSize),
		stopCh:    make(chan struct{}),
	}

	for _, opt := range opts {
		opt(p)
	}

	if err := p.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return p, nil
}

// Start запускает воркеры
func (p *Pool) Start(ctx context.Context) {
	const op = "worker.pool.Start"

	log := p.log.Ctx(ctx).With("op", op)
	log.LogAttrs(ctx, logger.InfoLevel, "starting worker pool",
		logger.Int("pool_size", p.poolSize),
		logger.Int("queue_size", p.queueSize),
	)

	for i := 0; i < p.poolSize; i++ {
		p.wg.Add(1)
		go p.worker(ctx, i)
	}

	log.LogAttrs(ctx, logger.InfoLevel, "worker pool started")
}

// worker — отдельная горутина-воркер
func (p *Pool) worker(ctx context.Context, id int) {
	defer p.wg.Done()

	const op = "worker.pool.worker"
	log := p.log.Ctx(ctx).With("op", op, "worker_id", id)

	log.LogAttrs(ctx, logger.DebugLevel, "worker started")

	for {
		select {
		case <-p.stopCh:
			log.LogAttrs(ctx, logger.DebugLevel, "worker stopped")
			return

		case task, ok := <-p.tasks:
			if !ok {
				log.LogAttrs(ctx, logger.DebugLevel, "task channel closed")
				return
			}

			p.processTask(ctx, id, task)
		}
	}
}

// processTask обрабатывает одну задачу с паник-рекавери
func (p *Pool) processTask(ctx context.Context, workerID int, task *Task) {
	const op = "worker.pool.processTask"

	log := p.log.Ctx(ctx).With("op", op, "task_id", task.ID, "worker_id", workerID)
	startTime := time.Now()

	// Обновляем статус в памяти
	procTask := &entity.Task{
		ID:        task.ID,
		ImageID:   task.ImageID,
		Status:    entity.TaskStatusProcessing,
		Progress:  10,
		CreatedAt: task.SubmittedAt,
	}
	p.statuses.Store(task.ID, procTask)

	// Паник-рекавери
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("panic recovered: %v", r)
			log.LogAttrs(ctx, logger.ErrorLevel, "panic in worker",
				logger.Any("panic", r),
			)
			procTask.MarkError(err.Error())
			p.statuses.Store(task.ID, procTask)
			if task.Err != nil {
				*task.Err = err
			}
			close(task.Done)
		}
	}()

	// Создаём контекст с таймаутом для задачи
	taskCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	log.LogAttrs(ctx, logger.InfoLevel, "processing task started")

	// Вызываем сервис для обработки
	// (в реальном проекте: отдельный метод service.ProcessTask)
	err := p.service.ProcessTask(taskCtx, task.ImageID, task.Options)

	if err != nil {
		log.LogAttrs(ctx, logger.ErrorLevel, "processing failed",
			logger.Any("error", err),
			logger.Duration("duration", time.Since(startTime)),
		)
		procTask.MarkError(err.Error())
		if task.Err != nil {
			*task.Err = err
		}
	} else {
		log.LogAttrs(ctx, logger.InfoLevel, "processing completed",
			logger.Duration("duration", time.Since(startTime)),
		)
		procTask.MarkDone()
	}

	p.statuses.Store(task.ID, procTask)
	close(task.Done)
}

// Submit добавляет задачу в очередь
func (p *Pool) Submit(ctx context.Context, task *entity.Task) error {
	const op = "worker.pool.Submit"

	log := p.log.Ctx(ctx).With("op", op, "task_id", task.ID)

	// Создаём обёртку задачи для воркера
	workerTask := &Task{
		ID:          task.ID,
		ImageID:     task.ImageID,
		Options:     task.Options,
		Status:      &task.Status,
		Err:         &task.ErrorMessage,
		Done:        make(chan struct{}),
		SubmittedAt: task.CreatedAt,
	}

	// Сохраняем начальный статус
	procTask := &entity.Task{
		ID:        task.ID,
		ImageID:   task.ImageID,
		Status:    entity.TaskStatusPending,
		Progress:  0,
		CreatedAt: task.CreatedAt,
	}
	p.statuses.Store(task.ID, procTask)

	select {
	case p.tasks <- workerTask:
		log.LogAttrs(ctx, logger.DebugLevel, "task submitted to queue")
		return nil

	case <-ctx.Done():
		return fmt.Errorf("%s: %w", op, ctx.Err())

	default:
		// Очередь переполнена
		return fmt.Errorf("%s: %w: queue full", op, entity.ErrProcessingFailed)
	}
}

// Status возвращает статус задачи
func (p *Pool) Status(id string) (*entity.Task, error) {
	const op = "worker.pool.Status"

	val, ok := p.statuses.Load(id)
	if !ok {
		return nil, entity.ErrTaskNotFound
	}

	task, ok := val.(*entity.Task)
	if !ok {
		return nil, fmt.Errorf("%s: invalid task type", op)
	}

	return task, nil
}

// Stop останавливает пул воркеров
func (p *Pool) Stop(ctx context.Context) {
	const op = "worker.pool.Stop"

	log := p.log.Ctx(ctx).With("op", op)
	log.LogAttrs(ctx, logger.InfoLevel, "stopping worker pool")

	p.stopOnce.Do(func() {
		close(p.stopCh)
		close(p.tasks)
	})

	// Ждём завершения воркеров с таймаутом
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.LogAttrs(ctx, logger.InfoLevel, "worker pool stopped gracefully")
	case <-ctx.Done():
		log.LogAttrs(ctx, logger.WarnLevel, "worker pool stop timeout",
			logger.Any("error", ctx.Err()),
		)
	case <-time.After(_shutdownTimeout):
		log.LogAttrs(ctx, logger.WarnLevel, "worker pool shutdown timeout")
	}
}

// Stats возвращает статистику пула
func (p *Pool) Stats() map[string]interface{} {
	stats := map[string]interface{}{
		"pool_size":   p.poolSize,
		"queue_size":  p.queueSize,
		"queue_len":   len(p.tasks),
		"active_tasks": 0,
	}

	// Считаем активные задачи
	p.statuses.Range(func(key, value interface{}) bool {
		if task, ok := value.(*entity.Task); ok {
			if !task.Status.IsTerminal() {
				stats["active_tasks"] = stats["active_tasks"].(int) + 1
			}
		}
		return true
	})

	return stats
}

// validate проверяет конфигурацию пула
func (p *Pool) validate() error {
	if p.poolSize <= 0 || p.poolSize > 64 {
		return fmt.Errorf("invalid pool size: %d", p.poolSize)
	}
	if p.queueSize <= 0 || p.queueSize > 10000 {
		return fmt.Errorf("invalid queue size: %d", p.queueSize)
	}
	if p.timeout <= 0 || p.timeout > 1*time.Hour {
		return fmt.Errorf("invalid task timeout: %v", p.timeout)
	}
	if p.service == nil {
		return fmt.Errorf("service is required")
	}
	return nil
}