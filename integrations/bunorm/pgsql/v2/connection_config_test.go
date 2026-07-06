package pgsql

import (
    "testing"
)

func TestConnectionConfigAccessors(t *testing.T) {
    connectionConfig := NewConnectionConfig("db.internal", "5432", "melody", "melody_user", "melody_password")

    if "db.internal" != connectionConfig.Host() {
        t.Fatalf("expected host db.internal, got %q", connectionConfig.Host())
    }
    if "5432" != connectionConfig.Port() {
        t.Fatalf("expected port 5432, got %q", connectionConfig.Port())
    }
    if "melody" != connectionConfig.Database() {
        t.Fatalf("expected database melody, got %q", connectionConfig.Database())
    }
    if "melody_user" != connectionConfig.User() {
        t.Fatalf("expected user melody_user, got %q", connectionConfig.User())
    }
    if "melody_password" != connectionConfig.Password() {
        t.Fatalf("expected password melody_password, got %q", connectionConfig.Password())
    }
}

/* @info SafeContext must redact the password while keeping the addressing fields */

func TestConnectionConfigSafeContextOmitsPassword(t *testing.T) {
    connectionConfig := NewConnectionConfig("db.internal", "5432", "melody", "melody_user", "melody_password")

    safeContext := connectionConfig.SafeContext()

    if "db.internal" != safeContext["host"] {
        t.Fatalf("expected host in the safe context, got %v", safeContext["host"])
    }
    if "5432" != safeContext["port"] {
        t.Fatalf("expected port in the safe context, got %v", safeContext["port"])
    }
    if "melody" != safeContext["database"] {
        t.Fatalf("expected database in the safe context, got %v", safeContext["database"])
    }
    if "melody_user" != safeContext["user"] {
        t.Fatalf("expected user in the safe context, got %v", safeContext["user"])
    }

    if _, found := safeContext["password"]; true == found {
        t.Fatalf("expected the password to be redacted from the safe context")
    }

    for key, value := range safeContext {
        if "melody_password" == value {
            t.Fatalf("expected no safe context value to leak the password, found it under %q", key)
        }
    }
}
