package cli

import (
    "bytes"
    "context"
    "database/sql"
    "errors"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/.example/twofactor"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    bun "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
)

/* the reset removes the enrollment of the account the name resolves to, by its identifier: the statement the store receives is the delete of that account's row and nothing else */
func TestTwoFactorResetCommand_DeletesTheEnrollmentOfTheNamedAccount(t *testing.T) {
    fixture := newCommandFixture(t)
    connector := &recordingResetConnector{}
    store := twofactor.NewStore(bun.NewDB(sql.OpenDB(connector), mysqldialect.New()))

    account, known, findErr := fixture.userService.FindByUsername(context.Background(), "editor")
    if nil != findErr || false == known {
        t.Fatalf("expected the seeded editor, got %v %v", known, findErr)
    }

    output := &bytes.Buffer{}
    command := NewTwoFactorResetCommand(fixture.lazyUserService(), func(runtimeInstance melodycontainercontract.Runtime) (*twofactor.Store, error) {
        return store, nil
    })

    runErr := command.Run(fixture.runtime, parsedContextValues{ArgumentValues: []string{"editor"}, WriterValue: output}.parse())
    if nil != runErr {
        t.Fatalf("expected the reset to succeed, got %v", runErr)
    }

    deleteList := make([]string, 0)
    for _, statement := range connector.recorded() {
        if true == strings.HasPrefix(statement, "DELETE ") {
            deleteList = append(deleteList, statement)
        }
    }

    if 2 != len(deleteList) || false == strings.Contains(deleteList[0], "melody_example_v4_two_factor_pending` WHERE (user_identifier = '"+account.Id+"')") || false == strings.Contains(deleteList[1], "melody_example_v4_two_factor` WHERE (user_identifier = '"+account.Id+"')") {
        t.Fatalf("expected the editor's pending and active enrollments deleted (%s), got %q", account.Id, connector.recorded())
    }

    if false == strings.Contains(output.String(), "removed the second factor") {
        t.Fatalf("expected the reset reported, got %q", output.String())
    }
}

/* an unknown name is refused before the store is asked */
func TestTwoFactorResetCommand_RefusesAnUnknownAccountBeforeTheStore(t *testing.T) {
    fixture := newCommandFixture(t)
    command := NewTwoFactorResetCommand(fixture.lazyUserService(), func(runtimeInstance melodycontainercontract.Runtime) (*twofactor.Store, error) {
        t.Fatal("expected the store never asked for an unknown account")

        return nil, nil
    })

    runErr := command.Run(fixture.runtime, parsedContextValues{ArgumentValues: []string{"nobody"}}.parse())
    if nil == runErr || false == strings.Contains(runErr.Error(), "does not exist") {
        t.Fatalf("expected an unknown account refused, got %v", runErr)
    }
}

func TestTwoFactorResetCommandReadsThroughItsContext(t *testing.T) {
    fixture := newInterruptedCommandFixture(t)
    command := NewTwoFactorResetCommand(fixture.lazyUserService(), func(runtimeInstance melodycontainercontract.Runtime) (*twofactor.Store, error) {
        t.Fatal("the interrupted reset reached the enrollment store")

        return nil, nil
    })

    if runErr := command.Run(fixture.runtime, parsedContextValues{ArgumentValues: []string{"editor"}}.parse()); false == errors.Is(runErr, context.Canceled) {
        t.Fatalf("expected the interrupted reset to end on its context, got %v", runErr)
    }
}

/* the reset reads the directory past the cache: an account deleted behind a cached entry is answered as unknown, never reset under an identifier the directory does not hold */
func TestTwoFactorResetCommand_ReadsTheDirectoryPastTheCache(t *testing.T) {
    fixture := newCommandFixture(t)
    connector := &recordingResetConnector{}
    store := twofactor.NewStore(bun.NewDB(sql.OpenDB(connector), mysqldialect.New()))

    account, known, findErr := fixture.userService.FindByUsername(context.Background(), "editor")
    if nil != findErr || false == known {
        t.Fatalf("expected the seeded editor cached, got %v %v", known, findErr)
    }

    if _, deleteErr := fixture.userRepository.DeleteById(context.Background(), account.Id, nil); nil != deleteErr {
        t.Fatalf("delete behind the cache: %v", deleteErr)
    }

    command := NewTwoFactorResetCommand(fixture.lazyUserService(), func(runtimeInstance melodycontainercontract.Runtime) (*twofactor.Store, error) {
        return store, nil
    })

    runErr := command.Run(fixture.runtime, parsedContextValues{ArgumentValues: []string{"editor"}, WriterValue: &bytes.Buffer{}}.parse())
    if nil == runErr || false == strings.Contains(runErr.Error(), "does not exist") {
        t.Fatalf("expected the deleted account answered unknown, got %v", runErr)
    }
}
