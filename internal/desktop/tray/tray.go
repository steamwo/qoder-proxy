package tray

type Callbacks struct {
	OpenWindow   func()
	ToggleProxy  func()
	RefreshQuota func()
	Quit         func()
}
type Status struct {
	Running  bool
	Tooltip  string
	Endpoint string
	Account  string
	Quota    string
}
type Tray interface {
	SetStatus(Status)
	Close() error
}

func Start(cb Callbacks) (Tray, error) { return start(cb) }
