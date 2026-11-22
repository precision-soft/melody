package pipeline

import (
    "slices"
)

type InactiveMiddleware struct {
    name   string
    reason string
}

func newInactiveMiddleware(name string, reason string) *InactiveMiddleware {
    return &InactiveMiddleware{name: name, reason: reason}
}

func (instance *InactiveMiddleware) Name() string { return instance.name }

func (instance *InactiveMiddleware) Reason() string { return instance.reason }

func newMiddlewareBuildReport(
    requestedGroup string,
    kernelEnvironment string,
    selectedNames []string,
    inactive []*InactiveMiddleware,
    missingReference []string,
    cycleDetected bool,
) *MiddlewareBuildReport {
    return &MiddlewareBuildReport{
        requestedGroup:    requestedGroup,
        kernelEnvironment: kernelEnvironment,
        selectedNames:     slices.Clone(selectedNames),
        inactive:          copyInactiveMiddlewareSlice(inactive),
        missingReference:  slices.Clone(missingReference),
        cycleDetected:     cycleDetected,
    }
}

type MiddlewareBuildReport struct {
    requestedGroup    string
    kernelEnvironment string
    selectedNames     []string
    inactive          []*InactiveMiddleware
    missingReference  []string
    cycleDetected     bool
}

func (instance *MiddlewareBuildReport) RequestedGroup() string {
    return instance.requestedGroup
}

func (instance *MiddlewareBuildReport) KernelEnvironment() string {
    return instance.kernelEnvironment
}

func (instance *MiddlewareBuildReport) SelectedNames() []string {
    return slices.Clone(instance.selectedNames)
}

func (instance *MiddlewareBuildReport) setSelectedNames(selectedNames []string) {
    instance.selectedNames = slices.Clone(selectedNames)
}

func (instance *MiddlewareBuildReport) Inactive() []*InactiveMiddleware {
    return copyInactiveMiddlewareSlice(instance.inactive)
}

/* setInactive stores a copy of the list, like every sibling accessor of this report. */
func (instance *MiddlewareBuildReport) setInactive(inactive []*InactiveMiddleware) {
    instance.inactive = copyInactiveMiddlewareSlice(inactive)
}

func (instance *MiddlewareBuildReport) MissingReference() []string {
    return slices.Clone(instance.missingReference)
}

func (instance *MiddlewareBuildReport) setMissingReference(missingReference []string) {
    instance.missingReference = slices.Clone(missingReference)
}

func (instance *MiddlewareBuildReport) CycleDetected() bool {
    return instance.cycleDetected
}

func (instance *MiddlewareBuildReport) setCycleDetected(cycleDetected bool) {
    instance.cycleDetected = cycleDetected
}

func copyInactiveMiddlewareSlice(values []*InactiveMiddleware) []*InactiveMiddleware {
    if nil == values {
        return nil
    }

    return append([]*InactiveMiddleware{}, values...)
}
