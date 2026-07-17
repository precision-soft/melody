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
    if commands := NewModule(ModuleConfig{}).RegisterCliCommands(nil); nil != commands {
        t.Fatalf("expected no commands for the fully-unset legacy shape, got %d", len(commands))
    }
}

func TestModule_RegisterCliCommandsPanicsOnPartialTopLevelConfig(t *testing.T) {
    cipher := NewCipher(NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)}))

    assertPanics(t, func() {
        NewModule(ModuleConfig{Database: newMysqlDatabase()}).RegisterCliCommands(nil)
    })

    assertPanics(t, func() {
        NewModule(ModuleConfig{Cipher: cipher}).RegisterCliCommands(nil)
    })
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

func TestModule_RegisterCliCommandsComposesLegacyAndContexts(t *testing.T) {
    database := newMysqlDatabase()

    module := NewModule(ModuleConfig{
        Database: database,
        Cipher:   NewFakeCipher(),
        Contexts: []CommandContextConfig{
            {Name: "platform", Database: database, Cipher: NewFakeCipher()},
            {Name: "payment", Database: database, Cipher: NewFakeCipher()},
        },
    })

    commands := module.RegisterCliCommands(nil)

    names := make(map[string]bool, len(commands))
    for _, command := range commands {
        names[command.Name()] = true
    }

    if false == names["melody:encrypt:database"] {
        t.Fatalf("expected the legacy unsuffixed command, got: %v", names)
    }

    if false == names["melody:encrypt:database:platform"] || false == names["melody:encrypt:database:payment"] {
        t.Fatalf("expected the per-context commands, got: %v", names)
    }
}

func TestModule_RegisterCliCommandsPanicsOnInvalidContext(t *testing.T) {
    database := newMysqlDatabase()

    assertPanics(t, func() {
        NewModule(ModuleConfig{
            Contexts: []CommandContextConfig{{Name: "", Database: database, Cipher: NewFakeCipher()}},
        }).RegisterCliCommands(nil)
    })

    assertPanics(t, func() {
        NewModule(ModuleConfig{
            Contexts: []CommandContextConfig{{Name: "payment"}},
        }).RegisterCliCommands(nil)
    })
}
