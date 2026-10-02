package cli

import (
    "bytes"
    "database/sql"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/.example/twofactor"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    bun "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
)

/* the reset removes the enrollment of the account the name resolves to, by its identifier: the statement the store receives is the delete of that account's row and nothing else */
func TestTwoFactorResetCommand_DeletesTheEnrollmentOfTheNamedAccount(t *testing.T) {
    fixture := newCommandFixture(t)
    connector := &recordingResetConnector{}
    store := twofactor.NewStore(bun.NewDB(sql.OpenDB(connector), mysqldialect.New()))

    account, known, findErr := fixture.userService.FindByUsername("editor")
    if nil != findErr || false == known {
        t.Fatalf("expected the seeded editor, got %v %v", known, findErr)
    }

    output := &bytes.Buffer{}
    command := NewTwoFactorResetCommand(fixture.lazyUserService(), func(runtimeInstance melodyruntimecontract.Runtime) (*twofactor.Store, error) {
        return store, nil
    })

    runErr := command.Run(fixture.runtime, &melodyclicontract.StaticContext{ArgumentValues: []string{"editor"}, WriterValue: output})
    if nil != runErr {
        t.Fatalf("expected the reset to succeed, got %v", runErr)
    }

    deleteList := make([]string, 0)
    for _, statement := range connector.recorded() {
        if true == strings.HasPrefix(statement, "DELETE ") {
            deleteList = append(deleteList, statement)
        }
    }

    if 1 != len(deleteList) || false == strings.Contains(deleteList[0], "'"+account.Id+"'") {
        t.Fatalf("expected one delete of the editor's enrollment (%s), got %q", account.Id, connector.recorded())
    }

    if false == strings.Contains(output.String(), "removed the second factor") {
        t.Fatalf("expected the reset reported, got %q", output.String())
    }
}

/* an unknown name is refused before the store is asked */
func TestTwoFactorResetCommand_RefusesAnUnknownAccountBeforeTheStore(t *testing.T) {
    fixture := newCommandFixture(t)
    command := NewTwoFactorResetCommand(fixture.lazyUserService(), func(runtimeInstance melodyruntimecontract.Runtime) (*twofactor.Store, error) {
        t.Fatal("expected the store never asked for an unknown account")

        return nil, nil
    })

    runErr := command.Run(fixture.runtime, &melodyclicontract.StaticContext{ArgumentValues: []string{"nobody"}})
    if nil == runErr || false == strings.Contains(runErr.Error(), "does not exist") {
        t.Fatalf("expected an unknown account refused, got %v", runErr)
    }
}
