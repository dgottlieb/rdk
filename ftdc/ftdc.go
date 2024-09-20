package ftdc

import (
	"context"
	"sync"
	"time"

	"go.viam.com/rdk/logging"
	"go.viam.com/utils"
)

type Statser interface {
	Stats() any
}

type namedStatser struct {
	name    string
	statser Statser
}

type FTDC struct {
	mu      sync.Mutex
	things  []namedStatser
	workers *utils.StoppableWorkers
	logger  logging.Logger
}

func New(logger logging.Logger) *FTDC {
	return &FTDC{
		logger: logger,
	}
}

func (ftdc *FTDC) Add(name string, statser Statser) {
	ftdc.mu.Lock()
	defer ftdc.mu.Unlock()
	ftdc.logger.Debug("Added:", name)
	ftdc.things = append(ftdc.things, namedStatser{
		name:    name,
		statser: statser,
	})
}

func (ftdc *FTDC) Remove(name string) {
	ftdc.logger.Debug("Removed:", name)
}

func (ftdc *FTDC) Start() {
	ftdc.workers = utils.NewStoppableWorkerWithTicker(time.Second, ftdc.doWork)
}

func (ftdc *FTDC) doWork(ctx context.Context) {
	metrics := map[string]any{
		"_time": time.Now(),
	}

	ftdc.mu.Lock()
	for idx := range ftdc.things {
		thing := &ftdc.things[idx]
		metrics[thing.name] = thing.statser.Stats()
	}
	ftdc.mu.Unlock()
	ftdc.logger.Debugw("Metrics collected.", "metrics", metrics)
}

func (ftdc *FTDC) StopAndJoin() {
	ftdc.workers.Stop()
}
