package repository

import (
    "strings"
    "testing"

    melodycontainer "github.com/precision-soft/melody/v3/container"
)

func TestSeedAll_RefusesAResolverHoldingNoSeeder(t *testing.T) {
    seedErr := SeedAll(t.Context(), melodycontainer.NewContainer())

    if nil == seedErr || false == strings.Contains(seedErr.Error(), "no nomenclature seeder is registered") {
        t.Fatalf("expected a resolver without seeders refused, got %v", seedErr)
    }
}
