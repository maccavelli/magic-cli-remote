package service

// taskNameFor is the scheduled-task name for a product, available on every
// platform so ProbeStatus can render a Windows hint without a build tag.
func taskNameFor(product string) string { return product }
