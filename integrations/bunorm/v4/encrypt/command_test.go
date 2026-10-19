package encrypt

import (
    "testing"

    "github.com/uptrace/bun"
)

/* the two doors are the whole command surface an application registers: an empty list would leave melody:encrypt:database unreachable while every module still boots, so the count and the name are what this pins */
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

/* the resolver form is the one an application registers at boot: the database is opened at the first run rather than at construction, so the resolver must NOT be called while the command list is being built */
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

/* both doors build a migrator, so both inherit its refusal of a cipher that cannot seal: without it the command would register cleanly and die at the first row of a bulk run, halfway through a table */
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
