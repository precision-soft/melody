package config

import (
    "testing"
)

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
