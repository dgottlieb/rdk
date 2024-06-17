package datasync

type noopManager struct{}

var _ Syncer = (*noopManager)(nil)

// NewNoopManager returns a noop sync manager that does nothing.
func NewNoopManager() Syncer {
	return &noopManager{}
}

func (m *noopManager) SyncFile(path string) {}

func (m *noopManager) SetArbitraryFileTags(tags []string) {}

func (m *noopManager) Close() {}

func (m *noopManager) MarkInProgress(path string) bool {
	return true
}

func (m *noopManager) SendFileToSync(path string) {}

func (m *noopManager) UnmarkInProgress(path string) {}
