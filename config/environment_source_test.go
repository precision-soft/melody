package config

import (
    "testing"

    configcontract "github.com/precision-soft/melody/config/contract"
)

func TestEnvironmentContractIsUsed(t *testing.T) {
    var _ configcontract.EnvironmentSource = (*testEnvironmentSource)(nil)
}
