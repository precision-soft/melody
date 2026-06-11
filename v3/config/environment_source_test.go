package config

import (
    "testing"

    configcontract "github.com/precision-soft/melody/v3/config/contract"
)

func TestEnvironmentContractIsUsed(t *testing.T) {
    var _ configcontract.EnvironmentSource = (*testEnvironmentSource)(nil)
}
