package pipeline

type InactiveMiddleware struct {
    name   string
    reason string
}

func NewInactiveMiddleware(name string, reason string) *InactiveMiddleware {
    return &InactiveMiddleware{name: name, reason: reason}
}

func (instance *InactiveMiddleware) Name() string { return instance.name }

func (instance *InactiveMiddleware) Reason() string { return instance.reason }

func NewMiddlewareBuildReport(
    requestedGroup string,
    kernelEnv string,
    selectedNames []string,
    inactive []*InactiveMiddleware,
    missingReference []string,
    cycleDetected bool,
) *MiddlewareBuildReport {
    return &MiddlewareBuildReport{
        requestedGroup:   requestedGroup,
        kernelEnv:        kernelEnv,
        selectedNames:    copyStringSlice(selectedNames),
        inactive:         copyInactiveMiddlewareSlice(inactive),
        missingReference: copyStringSlice(missingReference),
        cycleDetected:    cycleDetected,
    }
}

type MiddlewareBuildReport struct {
    requestedGroup   string
    kernelEnv        string
    selectedNames    []string
    inactive         []*InactiveMiddleware
    missingReference []string
    cycleDetected    bool
}

func (instance *MiddlewareBuildReport) RequestedGroup() string {
    return instance.requestedGroup
}

func (instance *MiddlewareBuildReport) KernelEnv() string {
    return instance.kernelEnv
}

func (instance *MiddlewareBuildReport) SelectedNames() []string {
    return copyStringSlice(instance.selectedNames)
}

func (instance *MiddlewareBuildReport) SetSelectedNames(selectedNames []string) {
    instance.selectedNames = copyStringSlice(selectedNames)
}

func (instance *MiddlewareBuildReport) Inactive() []*InactiveMiddleware {
    return copyInactiveMiddlewareSlice(instance.inactive)
}

func (instance *MiddlewareBuildReport) SetInactive(inactive []*InactiveMiddleware) {
    instance.inactive = inactive
}

func (instance *MiddlewareBuildReport) MissingReference() []string {
    return copyStringSlice(instance.missingReference)
}

func (instance *MiddlewareBuildReport) SetMissingReference(missingReference []string) {
    instance.missingReference = copyStringSlice(missingReference)
}

func (instance *MiddlewareBuildReport) CycleDetected() bool {
    return instance.cycleDetected
}

func (instance *MiddlewareBuildReport) SetCycleDetected(cycleDetected bool) {
    instance.cycleDetected = cycleDetected
}

func copyStringSlice(values []string) []string {
    if nil == values {
        return nil
    }

    return append([]string{}, values...)
}

func copyInactiveMiddlewareSlice(values []*InactiveMiddleware) []*InactiveMiddleware {
    if nil == values {
        return nil
    }

    return append([]*InactiveMiddleware{}, values...)
}
