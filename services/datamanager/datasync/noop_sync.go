package datasync

import (
	"context"

	v1 "go.viam.com/api/app/datasync/v1"
	"go.viam.com/rdk/internal/cloud"
)

type noopManager struct{}

var _ Syncer = (*noopManager)(nil)

// NewNoopManager returns a noop sync manager that does nothing.
func NewNoopManager() Syncer {
	return &noopManager{}
}

func (m *noopManager) Reconfigure(
	ctx context.Context,
	cloudConn cloud.ConnectionService,
	captureDir string,
	tags []string,
) {
}

func (m *noopManager) MarkInProgress(path string) bool {
	return true
}

func (m *noopManager) SyncFile(ctx context.Context, path string) {}

func (m *noopManager) UnmarkInProgress(path string) {}

func (m *noopManager) UseMockClient(client v1.DataSyncServiceClient) {}
