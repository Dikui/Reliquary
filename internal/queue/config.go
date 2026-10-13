package queue

import (
	"context"
	"math/rand/v2"
	"reliquary/internal/types"
	"sync/atomic"
	"time"
)

type Config struct {
	Pools                                                         types.WorkerPoolConcurrency
	CheckInterval, DeliveryTimeout                                time.Duration
	ReconnectInitialDelay, ReconnectMaxDelay, ReconnectAlertAfter time.Duration
	RetryDelayFunc                                                func(int, error, *Task) time.Duration
	OnDeadLetter                                                  func(context.Context, *Task, error)
}

func (c Config) withDefaults() Config {
	if c.Pools == (types.WorkerPoolConcurrency{}) {
		c.Pools = types.DefaultWorkerPoolConcurrency()
	}
	if c.CheckInterval <= 0 {
		c.CheckInterval = time.Minute
	}
	if c.DeliveryTimeout <= 0 {
		c.DeliveryTimeout = 2 * time.Minute
	}
	if c.ReconnectInitialDelay <= 0 {
		c.ReconnectInitialDelay = time.Second
	}
	if c.ReconnectMaxDelay < c.ReconnectInitialDelay {
		c.ReconnectMaxDelay = 30 * time.Second
	}
	if c.ReconnectAlertAfter <= 0 {
		c.ReconnectAlertAfter = 5 * time.Minute
	}
	if c.RetryDelayFunc == nil {
		c.RetryDelayFunc = DefaultRetryDelayFunc
	}
	return c
}

type workerPool struct {
	name        string
	concurrency int
	weights     map[string]int
	active      atomic.Int32
}

func (c Config) workerPools() []*workerPool {
	return []*workerPool{
		{name: types.WorkerPoolCore, concurrency: c.Pools.Core, weights: types.QueueWeightsForPool(types.WorkerPoolCore)},
		{name: types.WorkerPoolEnrichment, concurrency: c.Pools.Enrichment, weights: types.QueueWeightsForPool(types.WorkerPoolEnrichment)},
		{name: types.WorkerPoolMaintenance, concurrency: c.Pools.Maintenance, weights: types.QueueWeightsForPool(types.WorkerPoolMaintenance)},
		{name: types.WorkerPoolShared, concurrency: c.Pools.Shared, weights: types.QueueWeightsForSharedPool()},
	}
}
func DefaultRetryDelayFunc(n int, _ error, _ *Task) time.Duration {
	return time.Duration(n*n*n*n+15+rand.IntN(30)*(n+1)) * time.Second
}
