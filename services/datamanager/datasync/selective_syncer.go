package datasync

import (
	"context"

	"go.viam.com/rdk/services/datamanager"
)

type selectiveSyncer interface {
	Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error)
}

type neverSyncSensor struct{}

func (syncer neverSyncSensor) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{
		datamanager.ShouldSyncKey: false,
	}, nil
}
