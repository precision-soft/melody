package encrypt

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "testing"

    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
)

type fakeConnector struct{}

func (fakeConnector) Connect(context.Context) (driver.Conn, error) {
    return nil, errors.New("fake connector never connects")
}

func (fakeConnector) Driver() driver.Driver {
    return fakeDriver{}
}

type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) {
    return nil, errors.New("fake driver never opens")
}

func newMysqlDatabase() *bun.DB {
    return bun.NewDB(sql.OpenDB(fakeConnector{}), mysqldialect.New())
}

func TestModule_RegisterCliCommandsReturnsNilWithoutDependencies(t *testing.T) {
    cipher := NewCipher(NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)}))

    if commands := NewModule(ModuleConfig{}).RegisterCliCommands(nil); nil != commands {
        t.Fatalf("expected no commands without database or cipher, got %d", len(commands))
    }

    if commands := NewModule(ModuleConfig{Database: newMysqlDatabase()}).RegisterCliCommands(nil); nil != commands {
        t.Fatalf("expected no commands without cipher, got %d", len(commands))
    }

    if commands := NewModule(ModuleConfig{Cipher: cipher}).RegisterCliCommands(nil); nil != commands {
        t.Fatalf("expected no commands without database, got %d", len(commands))
    }
}

func TestModule_RegisterCliCommandsExposesEncryptDatabaseCommand(t *testing.T) {
    cipher := NewCipher(NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)}))

    commands := NewModule(ModuleConfig{Database: newMysqlDatabase(), Cipher: cipher}).RegisterCliCommands(nil)

    if 1 != len(commands) {
        t.Fatalf("expected exactly one command, got %d", len(commands))
    }

    if "melody:encrypt:database" != commands[0].Name() {
        t.Fatalf("expected the melody:encrypt:database command, got %q", commands[0].Name())
    }
}
