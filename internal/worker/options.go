package worker

import "time"

// Option функциональная опция для Pool
type Option func(*Pool)

// PoolSize устанавливает количество воркеров
func PoolSize(size int) Option {
	return func(p *Pool) {
		if size > 0 && size <= 64 {
			p.poolSize = size
		}
	}
}

// QueueSize устанавливает размер очереди задач
func QueueSize(size int) Option {
	return func(p *Pool) {
		if size > 0 && size <= 10000 {
			p.queueSize = size
		}
	}
}

// TaskTimeout устанавливает таймаут выполнения одной задачи
func TaskTimeout(timeout time.Duration) Option {
	return func(p *Pool) {
		if timeout > 0 && timeout <= 1*time.Hour {
			p.timeout = timeout
		}
	}
}