package worker

import "time"

type Option func(*Pool)

func PoolSize(n int) Option {
	return func(p *Pool) {
		if n > 0 && n <= 64 {
			p.poolSize = n
		}
	}
}

func QueueSize(n int) Option {
	return func(p *Pool) {
		if n > 0 && n <= 10000 {
			p.queueSize = n
		}
	}
}

func TaskTimeout(d time.Duration) Option {
	return func(p *Pool) {
		if d >= time.Second && d <= time.Hour {
			p.timeout = d
		}
	}
}
