package encrypt

import (
    "testing"

    "github.com/uptrace/bun"
)

func commandProbeCipher() Cipher {
    return NewCipher(NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)}))
}

func TestCommands_ExposesTheEncryptDatabaseCommand(t *testing.T) {
    commands := Commands(newMysqlDatabase(), commandProbeCipher())

    if 1 != len(commands) {
        t.Fatalf("expected exactly the one command, got %d", len(commands))
    }

    if "melody:encrypt:database" != commands[0].Name() {
        t.Fatalf("expected the encrypt database command, got %q", commands[0].Name())
    }
}

func TestCommandsFromResolver_DoesNotResolveTheDatabaseWhileBuildingTheList(t *testing.T) {
    resolved := false

    commands := CommandsFromResolver(func() (*bun.DB, error) {
        resolved = true

        return newMysqlDatabase(), nil
    }, commandProbeCipher())

    if true == resolved {
        t.Fatal("expected the database resolver to stay unrun until the command runs")
    }

    if 1 != len(commands) {
        t.Fatalf("expected exactly the one command, got %d", len(commands))
    }

    if "melody:encrypt:database" != commands[0].Name() {
        t.Fatalf("expected the encrypt database command, got %q", commands[0].Name())
    }
}

func TestCommands_RefuseACipherThatCanNotSeal(t *testing.T) {
    for _, probe := range []struct {
        name  string
        build func()
    }{
        {name: "Commands", build: func() { _ = Commands(newMysqlDatabase(), nil) }},
        {name: "CommandsFromResolver", build: func() {
            _ = CommandsFromResolver(func() (*bun.DB, error) { return newMysqlDatabase(), nil }, nil)
        }},
    } {
        func() {
            defer func() {
                if nil == recover() {
                    t.Fatalf("%s: expected a nil cipher to be refused", probe.name)
                }
            }()

            probe.build()
        }()
    }
}
