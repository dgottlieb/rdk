// Package sensor contains a gRPC based Sensor service serviceServer.
package sensor

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	commonpb "go.viam.com/api/common/v1"
	pb "go.viam.com/api/component/sensor/v1"

	"go.viam.com/rdk/protoutils"
	"go.viam.com/rdk/resource"
)

type counters struct {
	getReadings atomic.Int64
	doCommands  atomic.Int64
}

// ErrReadingsNil is the returned error if sensor readings are nil.
var ErrReadingsNil = func(sensorType, sensorName string) error {
	return fmt.Errorf("%v component %v Readings should not return nil readings", sensorType, sensorName)
}

// serviceServer implements the SensorService from sensor.proto.
type serviceServer struct {
	pb.UnimplementedSensorServiceServer
	coll resource.APIResourceCollection[Sensor]

	countersMu sync.Mutex
	counters   map[string]*counters
}

// NewRPCServiceServer constructs an sensor gRPC service serviceServer.
func NewRPCServiceServer(coll resource.APIResourceCollection[Sensor]) interface{} {
	return &serviceServer{
		coll:     coll,
		counters: make(map[string]*counters),
	}
}

func (s *serviceServer) getCounts(name string) *counters {
	s.countersMu.Lock()
	defer s.countersMu.Unlock()

	cnts, exists := s.counters[name]
	if exists {
		return cnts
	}

	cnts = new(counters)
	s.counters[name] = cnts
	return cnts
}

// GetReadings returns the most recent readings from the given Sensor.
func (s *serviceServer) GetReadings(
	ctx context.Context,
	req *commonpb.GetReadingsRequest,
) (*commonpb.GetReadingsResponse, error) {
	sensorDevice, err := s.coll.Resource(req.Name)
	if err != nil {
		return nil, err
	}
	readings, err := sensorDevice.Readings(ctx, req.Extra.AsMap())
	if err != nil {
		return nil, err
	}
	if readings == nil {
		return nil, ErrReadingsNil("sensor", req.Name)
	}
	m, err := protoutils.ReadingGoToProto(readings)
	if err != nil {
		return nil, err
	}

	s.getCounts(req.Name).getReadings.Add(1)
	return &commonpb.GetReadingsResponse{Readings: m}, nil
}

// DoCommand receives arbitrary commands.
func (s *serviceServer) DoCommand(ctx context.Context,
	req *commonpb.DoCommandRequest,
) (*commonpb.DoCommandResponse, error) {
	sensorDevice, err := s.coll.Resource(req.Name)
	if err != nil {
		return nil, err
	}

	s.getCounts(req.Name).doCommands.Add(1)
	return protoutils.DoFromResourceServer(ctx, sensorDevice, req)
}

type stats struct {
	getReadings int64
	doCommands  int64
}

func (s *serviceServer) Stats() any {
	s.countersMu.Lock()
	ret := make(map[string]stats, len(s.counters))
	for resName, counters := range s.counters {
		ret[resName] = stats{
			getReadings: counters.getReadings.Load(),
			doCommands:  counters.doCommands.Load(),
		}
	}
	s.countersMu.Unlock()

	fmt.Println("Stats called", ret)

	return ret
}
