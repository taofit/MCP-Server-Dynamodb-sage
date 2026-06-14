// Package queue provides a simple worker pool for background job processing.
package queue

import (
	"context"
	"sync"
	"time"
)

type Job func(ctx context.Context) error

type QueueManager struct {
	workerCount int
	buffer      int
	jobs        chan Job
	sync.WaitGroup
	runCancel context.CancelFunc
}

func New(workerCount, buffer int) *QueueManager {
	return &QueueManager{
		workerCount: workerCount,
		buffer:      buffer,
		jobs:        make(chan Job, buffer),
	}
}

func (m *QueueManager) Enqueue(job Job) error {
	m.jobs <- job
	return nil
}

func (m *QueueManager) Shutdown(ctx context.Context) {
	if m.runCancel != nil {
		m.runCancel()
	}
	done := make(chan struct{})
	go func() {
		close(m.jobs)
		m.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return
	case <-done:
		return
	}
}

func (m *QueueManager) Start(ctx context.Context) {
	runCtx, runCancel := context.WithCancel(ctx)
	m.runCancel = runCancel

	for i := 0; i < m.workerCount; i++ {
		m.Add(1)
		go func() {
			defer m.Done()
			for job := range m.jobs {
				job(runCtx)
			}
		}()
	}
}

func (m *QueueManager) Submit(job Job) {
	m.jobs <- job
}

func (m *QueueManager) Stop(ctx context.Context) {
	var cancel context.CancelFunc
	if ctx == nil {
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
	}
	m.Shutdown(ctx)
}
