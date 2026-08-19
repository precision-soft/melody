package bunorm

import (
    "testing"
)

func TestConnectionParametersSafeContextNamesTheConnection(t *testing.T) {
    connectionParameters := ConnectionParameters{
        Host:     "db.internal",
        Port:     "3306",
        Database: "melody",
        User:     "melody_user",
        Password: "melody_password",
    }

    safeContext := connectionParameters.SafeContext()

    if "db.internal" != safeContext["host"] {
        t.Fatalf("expected host in the safe context, got %v", safeContext["host"])
    }
    if "3306" != safeContext["port"] {
        t.Fatalf("expected port in the safe context, got %v", safeContext["port"])
    }
    if "melody" != safeContext["database"] {
        t.Fatalf("expected database in the safe context, got %v", safeContext["database"])
    }
    if "melody_user" != safeContext["user"] {
        t.Fatalf("expected user in the safe context, got %v", safeContext["user"])
    }
}

/* the value scan is what the assertion rests on: a password republished under any other key — "pass",
"credential", the connection string it was folded into — reads as redacted to a check that only looks
for the key it is not supposed to find. */
func TestConnectionParametersSafeContextOmitsPassword(t *testing.T) {
    connectionParameters := ConnectionParameters{
        Host:     "db.internal",
        Port:     "3306",
        Database: "melody",
        User:     "melody_user",
        Password: "melody_password",
    }

    safeContext := connectionParameters.SafeContext()

    if _, found := safeContext["password"]; true == found {
        t.Fatalf("expected the password to be redacted from the safe context")
    }

    for key, value := range safeContext {
        if "melody_password" == value {
            t.Fatalf("expected no safe context value to leak the password, found it under %q", key)
        }
    }
}

/* ConnectionParams has to stay an ALIAS rather than become a defined type: the drivers of the next major
still write it in the signatures that satisfy Provider, and a defined type would make those signatures
stop matching the contract while every file involved kept compiling on its own. */
func TestConnectionParamsIsAnAliasOfConnectionParameters(t *testing.T) {
    var connectionParameters ConnectionParameters = ConnectionParams{User: "melody_user"}

    if "melody_user" != connectionParameters.User {
        t.Fatalf("expected the alias to carry the same fields, got %q", connectionParameters.User)
    }
}
