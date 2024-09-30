package ftdc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/golang/snappy"
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

type Datum struct {
	// Public fields for json serializing
	Time int64
	Data map[string]any

	generationId int
}

type FTDC struct {
	json bool

	mu                 sync.Mutex
	things             []namedStatser
	thingsGenerationId int
	statsWorker        *utils.StoppableWorkers
	outputFormat       string
	statsCh            chan Datum

	outputWorkerDone chan struct{}
	logger           logging.Logger
}

type Schema struct{}

func New(logger logging.Logger) *FTDC {
	return &FTDC{
		outputFormat:     "json",
		statsCh:          make(chan Datum, 100),
		outputWorkerDone: make(chan struct{}),
		logger:           logger,
	}
}

func NewWithOutputFormat(logger logging.Logger, outputFormat string) *FTDC {
	return &FTDC{
		outputFormat:     outputFormat,
		statsCh:          make(chan Datum, 100),
		outputWorkerDone: make(chan struct{}),
		logger:           logger,
	}
}

func (ftdc *FTDC) Add(name string, statser Statser) {
	ftdc.mu.Lock()
	defer ftdc.mu.Unlock()

	for _, thing := range ftdc.things {
		if thing.name == name {
			ftdc.logger.Warnw("Trying to add conflicting ftdc section", "name", name)
			// FTDC output is broken down into separate "sections". The `name` is used to label each
			// section. We return here to predictably include one of the `Add`ed statsers.
			return
		}
	}

	ftdc.logger.Debugw("Added statser", "name", name, "type", fmt.Sprintf("%T", statser))
	ftdc.things = append(ftdc.things, namedStatser{
		name:    name,
		statser: statser,
	})
	ftdc.thingsGenerationId++
}

func (ftdc *FTDC) Remove(name string) {
	ftdc.mu.Lock()
	defer ftdc.mu.Unlock()
	for idx, thing := range ftdc.things {
		if thing.name == name {
			ftdc.logger.Debugw("Removed statser", "name", name, "type", fmt.Sprintf("%T", thing.statser))
			ftdc.things = append(ftdc.things[0:idx], ftdc.things[idx+1:len(ftdc.things)]...)
		}
	}

	ftdc.thingsGenerationId++
}

func (ftdc *FTDC) Start() {
	ftdc.statsWorker = utils.NewStoppableWorkerWithTicker(time.Second, ftdc.producerFn)
	go func() {
		outputFile, err := os.Create(fmt.Sprintf("./viam-server-%s.ftdc", ftdc.outputFormat))
		if err != nil {
			ftdc.logger.Warnw("FTDC failed to open file", "err", err)
			return
		}
		var writer io.WriteCloser = outputFile
		if ftdc.outputFormat == "json-snappy" {
			writer = snappy.NewBufferedWriter(outputFile)
		}
		defer func() {
			writer.Close()
			fmt.Println("Closing file:", outputFile.Name())
			if ftdc.outputFormat != "json-snappy" {
				outputFile.Close()
			}
			close(ftdc.outputWorkerDone)
		}()

		lastGenerationId := -1
		for datum := range ftdc.statsCh {
			var rewriteHeaders bool
			// If the generation id changed, our datum has a different schema. So we must recompute
			// headers.
			if lastGenerationId != datum.generationId {
				rewriteHeaders = true
			}

			if err := ftdc.output(datum, rewriteHeaders, writer); err != nil {
				break
			}
		}
	}()
}

func (ftdc *FTDC) producerFn(ctx context.Context) {
	datum := Datum{
		Time: time.Now().Unix(),
		Data: map[string]any{},
	}

	ftdc.mu.Lock()
	datum.generationId = ftdc.thingsGenerationId
	for idx := range ftdc.things {
		thing := &ftdc.things[idx]
		datum.Data[thing.name] = thing.statser.Stats()
	}
	ftdc.mu.Unlock()

	select {
	case ftdc.statsCh <- datum:
		break
	case <-ftdc.outputWorkerDone:
		break
	}

	// `Debugw` does not seem to serialize any of the `datum` value.
	ftdc.logger.Debugf("Metrics collected. Datum: %+v", datum)
}

func (ftdc *FTDC) output(datum Datum, rewriteHeaders bool, writer io.Writer) error {
	var err error
	ftdc.logger.Debugf("Outputting metrics. Datum: %+v", datum)
	datumBytes, err := json.Marshal(datum)
	if err != nil {
		ftdc.logger.Warnw("Failed to turn ftdc data into json", "err", err)
		return err
	}

	_, err = writer.Write(datumBytes)
	if err != nil {
		ftdc.logger.Warnw("Failed to write ftdc data to file", "err", err)
		return err
	}

	_, err = writer.Write([]byte("\n"))
	if err != nil {
		ftdc.logger.Warnw("Failed to write ftdc data to file", "err", err)
		return err
	}

	return nil
}

func (ftdc *FTDC) StopAndJoin() {
	ftdc.statsWorker.Stop()
	close(ftdc.statsCh)

	// Closing the `statsCh` signals to the `outputWorker` to complete and exit. We use a timeout to
	// limit how long we're willing to wait for the `outputWorker` to drain.
	select {
	case <-ftdc.outputWorkerDone:
	case <-time.After(10 * time.Second):
	}
}
