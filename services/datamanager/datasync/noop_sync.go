package datasync

import (
	"context"

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

func (m *noopManager) SendFileToSync(path string) {}

func (m *noopManager) UnmarkInProgress(path string) {}
