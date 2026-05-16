package config

import (
    melodyapplicationcontract "github.com/precision-soft/melody/application/contract"
)

type Module struct{}

func NewExampleModule() *Module {
    return &Module{}
}

func (instance *Module) Name() string {
    return "example"
}

func (instance *Module) Description() string {
    return "melody product catalog example application"
}

var _ melodyapplicationcontract.Module = (*Module)(nil)
