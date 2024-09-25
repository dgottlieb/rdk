package ftdc

import (
	"context"
	"encoding/json"
	"os"
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

type Datum map[string]any

type FTDC struct {
	mu          sync.Mutex
	things      []namedStatser
	statsWorker *utils.StoppableWorkers
	statsCh     chan Datum

	outputFile       *os.File
	outputWorkerDone chan struct{}
	logger           logging.Logger
}

func New(logger logging.Logger) *FTDC {
	return &FTDC{
		statsCh:          make(chan Datum, 100),
		outputWorkerDone: make(chan struct{}),
		logger:           logger,
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
	ftdc.statsWorker = utils.NewStoppableWorkerWithTicker(time.Second, ftdc.doWork)
	go func() {
		defer func() {
			if ftdc.outputFile != nil {
				ftdc.outputFile.Close()
			}
			close(ftdc.outputWorkerDone)
		}()

		for datum := range ftdc.statsCh {
			ftdc.output(datum)
		}
	}()
}

func (ftdc *FTDC) doWork(ctx context.Context) {
	var datum Datum = map[string]any{
		"_timeStart": time.Now().Unix(),
	}

	ftdc.mu.Lock()
	for idx := range ftdc.things {
		thing := &ftdc.things[idx]
		datum[thing.name] = thing.statser.Stats()
	}
	ftdc.mu.Unlock()

	datum["_timeEnd"] = time.Now().Unix()
	ftdc.statsCh <- datum
	ftdc.logger.Debugw("Metrics collected.", "metrics", datum)
}

func (ftdc *FTDC) output(datum Datum) {
	var err error
	if ftdc.outputFile == nil {
		ftdc.outputFile, err = os.Create("./viam-server.ftdc")
		if err != nil {
			ftdc.logger.Warnw("FTDC failed to open file", "err", err)
			return
		}
	}

	datumBytes, err := json.Marshal(datum)
	if err != nil {
		ftdc.logger.Warnw("Failed to turn ftdc data into json", "err", err)
		return
	}
	_, err = ftdc.outputFile.Write(datumBytes)
	if err != nil {
		ftdc.logger.Warnw("Failed to write ftdc data to file", "err", err)
		ftdc.outputFile.Close()
		ftdc.outputFile = nil
	}
}

func (ftdc *FTDC) StopAndJoin() {
	ftdc.statsWorker.Stop()
	close(ftdc.statsCh)

	// Closing the `statsCh` signals to the `outputWorker` to complete and exit. We use a timeout to
	// limit how long we're willing to wait for the `outputWorker` to drain.
	select {
	case <-ftdc.outputWorkerDone:
		if ftdc.outputFile != nil {
			_ = ftdc.outputFile.Close()
		}
	case <-time.After(10 * time.Second):
	}
}
