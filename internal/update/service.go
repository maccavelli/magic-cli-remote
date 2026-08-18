package update

// ServiceControl is the service-manager surface used by SwapAndRestart.
// Production uses internal/cli/service; tests inject fakes (0065 P1).
type ServiceControl interface {
	IsActive(product string) (bool, error)
	Stop(product string) error
	Start(product string) error
	// IsInstalled reports whether a service definition exists at all,
	// regardless of enabled or running state. Without it an update starts a
	// service that was never installed, fails, and rolls back a good swap
	// (MADR 0100 F3).
	IsInstalled(product string) (bool, error)
}

// UnitRefresh is what a post-swap definition reconciliation did.
type UnitRefresh struct {
	Changed    bool
	Path       string
	BackupPath string
	// Output is one already-formatted human line, or empty when there is
	// nothing worth saying.
	Output string
}

// UnitRefresher reconciles the installed service definition with the one the
// freshly swapped binary carries. It is implemented outside this package: the
// definition can only be rendered by the NEW binary, and update must stay free
// of template knowledge (0065 P1, MADR 0100).
type UnitRefresher interface {
	RefreshUnit(product, binary string) (UnitRefresh, error)
	RestoreUnit(product string, r UnitRefresh) error
}

// FuncService adapts functions into ServiceControl.
type FuncService struct {
	IsActiveFn    func(product string) (bool, error)
	StopFn        func(product string) error
	StartFn       func(product string) error
	IsInstalledFn func(product string) (bool, error)
}

// IsActive implements ServiceControl.
func (f FuncService) IsActive(product string) (bool, error) {
	if f.IsActiveFn == nil {
		return false, nil
	}
	return f.IsActiveFn(product)
}

// Stop implements ServiceControl.
func (f FuncService) Stop(product string) error {
	if f.StopFn == nil {
		return nil
	}
	return f.StopFn(product)
}

// Start implements ServiceControl.
func (f FuncService) Start(product string) error {
	if f.StartFn == nil {
		return nil
	}
	return f.StartFn(product)
}

// IsInstalled implements ServiceControl. A nil func answers false, which keeps
// an unconfigured caller from starting a service it never installed.
func (f FuncService) IsInstalled(product string) (bool, error) {
	if f.IsInstalledFn == nil {
		return false, nil
	}
	return f.IsInstalledFn(product)
}
