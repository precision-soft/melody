package repository

import (
    "strings"
    "testing"

    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
)

func TestSeedAll_RefusesARuntimeHoldingNoSeeder(t *testing.T) {
    seedErr := SeedAll(melodyruntime.NewRun(t.Context(), melodycontainer.NewContainer(melodycontainer.ContainerConfig{}).NewScope()))

    if nil == seedErr || false == strings.Contains(seedErr.Error(), "no nomenclature seeder is registered") {
        t.Fatalf("expected a runtime without seeders refused, got %v", seedErr)
    }
}
