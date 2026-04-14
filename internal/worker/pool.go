package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"img-processor/internal/entity"

	"github.com/wb-go/wbf/logger"
)

const (
	_defaultPoolSize  = 4
	_defaultQueueSize = 100
	_taskTimeout      = 10 * time.Minute
	_shutdownTimeout  = 30 * time.Second
	_progressStarted  = 10
)

type TaskProcessor interface {
	ProcessTask(ctx context.Context, task *entity.Task) error
}

type workerTask struct {
	ID          string
	ImageID     string
	Options     *entity.ProcessingOptions
	Err         *error
	Done        chan struct{}
	SubmittedAt time.Time
}

type Pool struct {
	processor TaskProcessor
	log       logger.Logger

	poolSize  int
	queueSize int
	timeout   time.Duration

	tasks    chan *workerTask
	statuses sync.Map
	wg       sync.WaitGroup
	stopOnce sync.Once
	stopCh   chan struct{}
}

func NewPool(proc TaskProcessor, log logger.Logger, opts ...Option) (*Pool, error) {
	const op = "worker.NewPool"

	p := &Pool{
		processor: proc,
		log:       log,
		poolSize:  _defaultPoolSize,
		queueSize: _defaultQueueSize,
		timeout:   _taskTimeout,
		tasks:     make(chan *workerTask, _defaultQueueSize),
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

func (p *Pool) Start(ctx context.Context) {
	const op = "worker.Start"
	log := p.log.Ctx(ctx).With("op", op)

	log.LogAttrs(ctx, logger.InfoLevel, "starting worker pool",
		logger.Int("pool_size", p.poolSize),
		logger.Int("queue_size", p.queueSize),
	)

	for i := range p.poolSize {
		p.wg.Add(1)
		go p.worker(ctx, i)
	}
}

func (p *Pool) worker(ctx context.Context, id int) {
	defer p.wg.Done()
	log := p.log.Ctx(ctx).With("worker_id", id)

	log.LogAttrs(ctx, logger.DebugLevel, "worker started")

	for {
		select {
		case <-p.stopCh:
			log.LogAttrs(ctx, logger.DebugLevel, "worker stopped")
			return
		case wt, ok := <-p.tasks:
			if !ok {
				log.LogAttrs(ctx, logger.DebugLevel, "task channel closed")
				return
			}
			p.processTask(ctx, id, wt)
		}
	}
}

func (p *Pool) processTask(ctx context.Context, workerID int, wt *workerTask) {
	const op = "worker.processTask"
	log := p.log.Ctx(ctx).With("op", op, "task_id", wt.ID, "worker_id", workerID)
	startTime := time.Now()

	procTask := &entity.Task{
		ID:        wt.ID,
		ImageID:   wt.ImageID,
		Status:    entity.TaskStatusProcessing,
		Progress:  _progressStarted,
		Options:   wt.Options,
		CreatedAt: wt.SubmittedAt,
	}
	p.statuses.Store(wt.ID, procTask)

	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("panic recovered: %v", r)
			log.LogAttrs(ctx, logger.ErrorLevel, "panic in worker", logger.Any("panic", r))
			procTask.MarkError(err.Error())
			if wt.Err != nil && *wt.Err == nil {
				*wt.Err = err
			}
			close(wt.Done)
		}
	}()

	taskCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	log.LogAttrs(ctx, logger.InfoLevel, "processing started")

	err := p.processor.ProcessTask(taskCtx, procTask)
	if err != nil {
		log.LogAttrs(ctx, logger.ErrorLevel, "processing failed",
			logger.Any("error", err),
			logger.Duration("duration", time.Since(startTime)),
		)
		procTask.MarkError(err.Error())
		if wt.Err != nil {
			*wt.Err = err
		}
	} else {
		log.LogAttrs(ctx, logger.InfoLevel, "processing completed",
			logger.Duration("duration", time.Since(startTime)),
		)
		procTask.MarkDone()
	}

	close(wt.Done)
}

func (p *Pool) Submit(ctx context.Context, task *entity.Task) error {
	const op = "worker.Submit"
	log := p.log.Ctx(ctx).With("op", op, "task_id", task.ID)

	var taskErr error
	wt := &workerTask{
		ID:          task.ID,
		ImageID:     task.ImageID,
		Options:     task.Options,
		Err:         &taskErr,
		Done:        make(chan struct{}),
		SubmittedAt: task.CreatedAt,
	}

	procTask := &entity.Task{
		ID:        task.ID,
		ImageID:   task.ImageID,
		Status:    entity.TaskStatusPending,
		Progress:  0,
		Options:   task.Options,
		CreatedAt: task.CreatedAt,
	}
	p.statuses.Store(task.ID, procTask)

	select {
	case p.tasks <- wt:
		log.LogAttrs(ctx, logger.DebugLevel, "task submitted to queue")
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", op, ctx.Err())
	default:
		return fmt.Errorf("%s: %w: queue full", op, entity.ErrProcessingFailed)
	}
}

func (p *Pool) Status(id string) (*entity.Task, error) {
	const op = "worker.Status"
	val, ok := p.statuses.Load(id)
	if !ok {
		return nil, entity.ErrTaskNotFound
	}
	task, ok := val.(*entity.Task)
	if !ok {
		return nil, fmt.Errorf("%s: invalid task type in statuses map", op)
	}
	return task, nil
}

func (p *Pool) Stop(ctx context.Context) {
	const op = "worker.Stop"
	log := p.log.Ctx(ctx).With("op", op)

	log.LogAttrs(ctx, logger.InfoLevel, "stopping worker pool")

	p.stopOnce.Do(func() {
		close(p.stopCh)
		close(p.tasks)
	})

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.LogAttrs(ctx, logger.InfoLevel, "worker pool stopped gracefully")
	case <-ctx.Done():
		log.LogAttrs(ctx, logger.WarnLevel, "worker pool stop interrupted",
			logger.Any("error", ctx.Err()))
	case <-time.After(_shutdownTimeout):
		log.LogAttrs(ctx, logger.WarnLevel, "worker pool shutdown timeout")
	}
}

func (p *Pool) validate() error {
	if p.poolSize <= 0 || p.poolSize > 64 {
		return fmt.Errorf("invalid pool size: %d (must be 1-64)", p.poolSize)
	}
	if p.queueSize <= 0 || p.queueSize > 10000 {
		return fmt.Errorf("invalid queue size: %d (must be 1-10000)", p.queueSize)
	}
	if p.timeout < time.Second || p.timeout > time.Hour {
		return fmt.Errorf("invalid task timeout: %v (must be 1s-1h)", p.timeout)
	}
	if p.processor == nil {
		return errors.New("processor (TaskProcessor) is required")
	}
	return nil
}
