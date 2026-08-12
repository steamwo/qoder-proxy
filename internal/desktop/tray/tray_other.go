//go:build !windows && !linux

package tray

type noop struct{}

func start(Callbacks) (Tray, error) { return &noop{}, nil }
func (*noop) SetStatus(Status)      {}
func (*noop) Close() error          { return nil }
