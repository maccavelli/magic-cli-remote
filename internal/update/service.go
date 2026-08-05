package update

// ServiceControl is the service-manager surface used by SwapAndRestart.
// Production uses internal/cli/service; tests inject fakes (0065 P1).
type ServiceControl interface {
	IsActive(product string) (bool, error)
	Stop(product string) error
	Start(product string) error
}

// FuncService adapts three functions into ServiceControl.
type FuncService struct {
	IsActiveFn func(product string) (bool, error)
	StopFn     func(product string) error
	StartFn    func(product string) error
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
