package pipeline

import (
    "slices"
)

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
        selectedNames:    slices.Clone(selectedNames),
        inactive:         copyInactiveMiddlewareSlice(inactive),
        missingReference: slices.Clone(missingReference),
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
    return slices.Clone(instance.selectedNames)
}

func (instance *MiddlewareBuildReport) SetSelectedNames(selectedNames []string) {
    instance.selectedNames = slices.Clone(selectedNames)
}

func (instance *MiddlewareBuildReport) Inactive() []*InactiveMiddleware {
    return copyInactiveMiddlewareSlice(instance.inactive)
}

/* SetInactive copies like every sibling accessor of this report: retaining the caller's slice was the one asymmetry, and a caller reusing its slice rewrote the stored report behind the getter's copy. */
func (instance *MiddlewareBuildReport) SetInactive(inactive []*InactiveMiddleware) {
    instance.inactive = copyInactiveMiddlewareSlice(inactive)
}

func (instance *MiddlewareBuildReport) MissingReference() []string {
    return slices.Clone(instance.missingReference)
}

func (instance *MiddlewareBuildReport) SetMissingReference(missingReference []string) {
    instance.missingReference = slices.Clone(missingReference)
}

func (instance *MiddlewareBuildReport) CycleDetected() bool {
    return instance.cycleDetected
}

func (instance *MiddlewareBuildReport) SetCycleDetected(cycleDetected bool) {
    instance.cycleDetected = cycleDetected
}


func copyInactiveMiddlewareSlice(values []*InactiveMiddleware) []*InactiveMiddleware {
    if nil == values {
        return nil
    }

    return append([]*InactiveMiddleware{}, values...)
}
