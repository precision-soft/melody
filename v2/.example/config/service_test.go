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

/* The empty answer is the switch: registerSessionStorage registers nothing for it and the framework's in-memory default stays. Anything else must come back absolute, anchored to the project directory when the configuration spelled it relative. */
func TestResolvedSessionFilePathKeepsTheEmptySwitchEmpty(t *testing.T) {
    if "" != resolvedSessionFilePath("", "/srv/example") {
        t.Fatalf("an empty value did not stay empty")
    }
}

func TestResolvedSessionFilePathAnchorsARelativePathToTheProjectDirectory(t *testing.T) {
    resolved := resolvedSessionFilePath("var/session/session.json", "/srv/example")

    if "/srv/example/var/session/session.json" != resolved {
        t.Fatalf("unexpected path: %q", resolved)
    }
}

func TestResolvedSessionFilePathKeepsAnAbsolutePath(t *testing.T) {
    resolved := resolvedSessionFilePath("/tmp/session.json", "/srv/example")

    if "/tmp/session.json" != resolved {
        t.Fatalf("unexpected path: %q", resolved)
    }
}
