package config

import (
    "testing"

    melodybunorm "github.com/precision-soft/melody/integrations/bunorm/v2"
)

/* the empty answer is how this module tells the repositories there is no database: every repository provider reads it and hands back its in-memory implementation instead. A name answered without a registry would send them looking for a service nothing registered, and the failure would surface at the first request rather than at wiring. */
func TestDatabaseServiceNameIsEmptyWithoutARegistry(t *testing.T) {
    module := &Module{}

    if "" != module.databaseServiceName() {
        t.Fatalf("expected no database service name, got %q", module.databaseServiceName())
    }
}

func TestDatabaseServiceNameNamesTheConnectionOnceOneIsBuilt(t *testing.T) {
    module := &Module{databaseRegistry: &melodybunorm.ManagerRegistry{}}

    if ServiceExampleDatabase != module.databaseServiceName() {
        t.Fatalf("expected %q, got %q", ServiceExampleDatabase, module.databaseServiceName())
    }
}
