package encrypt

import (
    "context"
    "testing"

    "github.com/uptrace/bun"
)

func commandProbeCipher() Cipher {
    return NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}}))
}

func TestCommands_ExposesTheEncryptDatabaseCommand(t *testing.T) {
    commands := Commands(commandProbeCipher(), EncryptDatabaseCommandConfig{Database: newMysqlDatabase()})

    if 1 != len(commands) {
        t.Fatalf("expected exactly the one command, got %d", len(commands))
    }

    if "melody:encrypt:database" != commands[0].Name() {
        t.Fatalf("expected the encrypt database command, got %q", commands[0].Name())
    }
}

func TestCommands_DoesNotResolveTheDatabaseWhileBuildingTheList(t *testing.T) {
    resolved := false

    commands := Commands(commandProbeCipher(), EncryptDatabaseCommandConfig{
        DatabaseResolver: func(ctx context.Context) (*bun.DB, error) {
            resolved = true

            return newMysqlDatabase(), nil
        },
        CommandName: "melody:encrypt:database:crm",
    })

    if true == resolved {
        t.Fatal("expected the database resolver to stay unrun until the command runs")
    }

    if 1 != len(commands) || "melody:encrypt:database:crm" != commands[0].Name() {
        t.Fatalf("expected the one named command, got %+v", commands)
    }
}

func TestCommands_RefuseACipherThatCanNotSeal(t *testing.T) {
    assertPanicsWithMessage(t, "encrypt database command cipher is nil", func() {
        _ = Commands(nil, EncryptDatabaseCommandConfig{Database: newMysqlDatabase()})
    })
}
