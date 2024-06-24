package datasync

import (
	"sync"

	"go.viam.com/rdk/logging"
)

type Workers struct {
	mu                  sync.Mutex
	parentWaitGroup     *sync.WaitGroup
	numWorkers          int
	requestedNumWorkers int
	singleWorkItemFn    func()
	logger              logging.Logger
}

// NewWorkers returns an object that manages goroutines of sync work items. The returned object is
// dormant until `SetNumWorkers` is invoked with a positive integer.
func NewWorkers(singleWorkItemFn func(), parentWaitGroup *sync.WaitGroup, logger logging.Logger) *Workers {
	return &Workers{singleWorkItemFn: singleWorkItemFn, parentWaitGroup: parentWaitGroup, logger: logger}
}

// SetNumWorkers changes the intended pool size of worker goroutines.
//
// If the requested number of workers is larger than the active number of workers, new workers will
// be created.
//
// If the intended value is smaller than the number of active workers, the function will return
// prior to any draining. Existing workers will eventually exit until the desired number of workers
// is reached.
func (workers *Workers) SetNumWorkers(requestedNumWorkers int) {
	workers.mu.Lock()
	defer workers.mu.Unlock()
	if workers.requestedNumWorkers == requestedNumWorkers {
		return
	}
	workers.requestedNumWorkers = requestedNumWorkers

	workers.logger.Infow("Changing number of datasync workers",
		"oldNumWorkers", workers.requestedNumWorkers,
		"newNumWorkers", requestedNumWorkers)

	for toAdd := workers.numWorkers; toAdd < requestedNumWorkers; toAdd++ {
		workers.numWorkers++
		workers.parentWaitGroup.Add(1)
		go func() {
			defer workers.parentWaitGroup.Done()
			for {
				workers.mu.Lock()
				if workers.numWorkers > workers.requestedNumWorkers {
					workers.numWorkers--
					workers.mu.Unlock()
					return
				}
				workers.mu.Unlock()

				workers.singleWorkItemFn()
			}
		}()
	}
}
