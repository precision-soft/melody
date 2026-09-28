package user

import (
    "errors"
    "fmt"
    nethttp "net/http"
    "testing"

    "github.com/precision-soft/melody/.example/repository"
)

func TestUserWriteRefusalStatusAnswersTheUniqueKeysRefusalOfATakenUsernameAs400(t *testing.T) {
    status, message := userWriteRefusalStatus(fmt.Errorf("audited insert failed: %w", repository.ErrUsernameAlreadyExists), "failed to create user")
    if nethttp.StatusBadRequest != status || "username already exists" != message {
        t.Fatalf("expected 400 username already exists, got %d %q", status, message)
    }
}

func TestUserWriteRefusalStatusAnswersAnyOtherFailureAsTheDoors(t *testing.T) {
    status, message := userWriteRefusalStatus(errors.New("connection refused"), "failed to update user")
    if nethttp.StatusInternalServerError != status || "failed to update user" != message {
        t.Fatalf("expected 500 failed to update user, got %d %q", status, message)
    }
}
