//nolint:musttag
package kafkat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"img-processor/internal/config"
	"img-processor/internal/entity"
	"img-processor/internal/service"

	"github.com/segmentio/kafka-go"
	"github.com/wb-go/wbf/kafka/dlq"
	kafkav2 "github.com/wb-go/wbf/kafka/kafka-v2"
	"github.com/wb-go/wbf/logger"
)

type KafkaAdapter struct {
	producer  *kafkav2.Producer
	processor *kafkav2.Processor
	svc       *service.ProcessorService
	log       logger.Logger
}

func NewKafkaAdapter(
	cfg config.Kafka,
	log logger.Logger,
) (*KafkaAdapter, error) {
	prod := kafkav2.NewProducer(cfg.Brokers, cfg.Topic, log)

	dlqProd := kafkav2.NewProducer(cfg.Brokers, cfg.DLQTopic, log)
	dlqClient := dlq.New(dlqProd, log)

	cons := kafkav2.NewConsumer(cfg.Brokers, cfg.Topic, cfg.GroupID, log)

	proc, err := kafkav2.NewProcessor(cons, dlqClient, log,
		kafkav2.MaxAttempts(cfg.MaxAttempts),
		kafkav2.BaseRetryDelay(cfg.BaseRetryDelay),
		kafkav2.MaxRetryDelay(cfg.MaxRetryDelay),
	)
	if err != nil {
		return nil, fmt.Errorf("init kafka processor: %w", err)
	}

	adapter := &KafkaAdapter{
		producer:  prod,
		processor: proc,
		log:       log,
	}
	return adapter, nil
}

func (a *KafkaAdapter) SetProcessorService(svc *service.ProcessorService) {
	a.svc = svc
}

func (a *KafkaAdapter) Publish(ctx context.Context, task *entity.Task) error {
	payload, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("marshal task: %w", err)
	}

	err = a.producer.Send(ctx, []byte(task.ImageID.String()), payload)
	if err != nil {
		return fmt.Errorf("kafka publish: %w", err)
	}
	return nil
}

func (a *KafkaAdapter) Start(ctx context.Context) {
	handler := func(ctx context.Context, msg kafka.Message) error {
		if a.svc == nil {
			a.log.Error("ProcessorService is not initialized in KafkaAdapter")
			return errors.New("processor service not initialized")
		}

		var task entity.Task
		if err := json.Unmarshal(msg.Value, &task); err != nil {
			return fmt.Errorf("unmarshal task: %w", err)
		}
		return a.svc.ProcessTask(ctx, &task)
	}
	a.log.LogAttrs(ctx, logger.InfoLevel, "kafka consumer started")
	a.processor.Start(ctx, handler)
}

func (a *KafkaAdapter) Close() error {
	err := a.producer.Close()
	if err != nil {
		return fmt.Errorf("kafka producer close: %w", err)
	}
	return nil
}
