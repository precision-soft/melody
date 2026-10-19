package security

import (
    "errors"
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func TestPasswordAuthenticator_SupportsOnlyARequestCarryingBothCredentials(t *testing.T) {
    authenticator := NewPasswordAuthenticator(nil)

    if false == authenticator.Supports(passwordRequest(t, "editor", "editor")) {
        t.Fatal("expected a request carrying both credentials to be supported")
    }

    if true == authenticator.Supports(passwordRequest(t, "editor", "")) {
        t.Fatal("expected a request without a password to be unsupported")
    }

    if true == authenticator.Supports(passwordRequest(t, "", "editor")) {
        t.Fatal("expected a request without a username to be unsupported")
    }
}

func TestPasswordAuthenticator_PublishesTheAcceptedAccountUnderItsIdentity(t *testing.T) {
    account := entity.NewUser("user-2", "editor", testPasswordHash, []string{"ROLE_EDITOR"})

    authenticator := NewPasswordAuthenticator(func(runtimeInstance melodyruntimecontract.Runtime, username string, password string) (*entity.User, bool, error) {
        if "editor" != username || "secret" != password {
            t.Fatalf("expected the credentials the door set, got %q / %q", username, password)
        }

        return account, true, nil
    })

    request := passwordRequest(t, "editor", "secret")

    token, authenticateErr := authenticator.Authenticate(request)
    if nil != authenticateErr {
        t.Fatalf("authenticate: %v", authenticateErr)
    }

    if false == token.IsAuthenticated() || "user-2" != token.UserIdentifier() {
        t.Fatalf("expected an authenticated token for user-2, got %+v", token)
    }

    published, found := LoginAccount(request)
    if false == found || account != published {
        t.Fatalf("expected the accepted account published on the request, got %v %v", published, found)
    }
}

func TestPasswordAuthenticator_AnswersAnonymousAndPublishesNothingForARefusedPassword(t *testing.T) {
    authenticator := NewPasswordAuthenticator(func(runtimeInstance melodyruntimecontract.Runtime, username string, password string) (*entity.User, bool, error) {
        return nil, false, nil
    })

    request := passwordRequest(t, "editor", "wrong")

    token, authenticateErr := authenticator.Authenticate(request)
    if nil != authenticateErr {
        t.Fatalf("authenticate: %v", authenticateErr)
    }

    if true == token.IsAuthenticated() {
        t.Fatal("expected a refused password to answer an unauthenticated token")
    }

    if _, found := LoginAccount(request); true == found {
        t.Fatal("expected no account published for a refused password")
    }
}

func TestPasswordAuthenticator_AnswersTheCheckFailureAsAnError(t *testing.T) {
    checkErr := errors.New("the account directory is unavailable")

    authenticator := NewPasswordAuthenticator(func(runtimeInstance melodyruntimecontract.Runtime, username string, password string) (*entity.User, bool, error) {
        return nil, false, checkErr
    })

    _, authenticateErr := authenticator.Authenticate(passwordRequest(t, "editor", "secret"))
    if false == errors.Is(authenticateErr, checkErr) {
        t.Fatalf("expected the check failure, got %v", authenticateErr)
    }
}
