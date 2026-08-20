package application

import (
    "time"

    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
)

func NewProcessContext(processId string, startedAt time.Time) *ProcessContext {
    return &ProcessContext{
        processId: processId,
        startedAt: startedAt,
    }
}

type ProcessContext struct {
    processId string
    startedAt time.Time
}

func (instance *ProcessContext) ProcessId() string {
    return instance.processId
}

func (instance *ProcessContext) StartedAt() time.Time {
    return instance.startedAt
}

var _ applicationcontract.ProcessContext = (*ProcessContext)(nil)
