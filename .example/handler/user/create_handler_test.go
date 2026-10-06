package user

import (
    "errors"
    "fmt"
    nethttp "net/http"
    "strings"
    "testing"

    "github.com/precision-soft/melody/.example/entity"
    "github.com/precision-soft/melody/.example/repository"
    "github.com/precision-soft/melody/.example/security"
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

/* bcrypt reads 72 bytes of the plaintext: the 72nd is the last a sign-in can tell apart, so the door takes it and refuses the 73rd */
func TestApiCreateDoor_AcceptsA72BytePasswordAndRefuses73(t *testing.T) {
    fixture := newUserDoorFixture(t)

    status, body := fixture.call(t, ApiCreateHandler(), nethttp.MethodPost, `{"username":"refused-73","password":"`+strings.Repeat("p", security.PasswordMaximumBytes+1)+`","roles":["`+entity.RoleUser+`"]}`, nil)
    if nethttp.StatusBadRequest != status || false == strings.Contains(body, "72 bytes") {
        t.Fatalf("the 73-byte password answered %d: %s", status, body)
    }

    status, body = fixture.call(t, ApiCreateHandler(), nethttp.MethodPost, `{"username":"accepted-72","password":"`+strings.Repeat("p", security.PasswordMaximumBytes)+`","roles":["`+entity.RoleUser+`"]}`, nil)
    if nethttp.StatusCreated != status {
        t.Fatalf("the 72-byte password answered %d: %s", status, body)
    }

    if true == fixture.holds(t, "refused-73") || false == fixture.holds(t, "accepted-72") {
        t.Fatal("the directory does not hold what the door answered")
    }
}

func TestApiCreateDoor_AcceptsA255ByteUsernameAndRefuses256(t *testing.T) {
    fixture := newUserDoorFixture(t)

    status, body := fixture.call(t, ApiCreateHandler(), nethttp.MethodPost, `{"username":"`+strings.Repeat("a", 256)+`","password":"a-password","roles":["`+entity.RoleUser+`"]}`, nil)
    if nethttp.StatusBadRequest != status || false == strings.Contains(body, "within 255 bytes") {
        t.Fatalf("the 256-byte username answered %d: %s", status, body)
    }

    username := strings.Repeat("a", 255)
    status, body = fixture.call(t, ApiCreateHandler(), nethttp.MethodPost, `{"username":"`+username+`","password":"a-password","roles":["`+entity.RoleUser+`"]}`, nil)
    if nethttp.StatusCreated != status || false == fixture.holds(t, username) {
        t.Fatalf("the 255-byte username answered %d: %s", status, body)
    }
}

func TestApiCreateDoor_AnswersABodyPastTheLimit413(t *testing.T) {
    fixture := newUserDoorFixture(t)
    fixture.bodyLimit = 8

    if status, body := fixture.call(t, ApiCreateHandler(), nethttp.MethodPost, `{"username":"bounded-account","password":"a-password"}`, nil); nethttp.StatusRequestEntityTooLarge != status {
        t.Fatalf("expected 413, got %d %s", status, body)
    }

    if true == fixture.holds(t, "bounded-account") {
        t.Fatal("expected nothing stored from a body never read")
    }
}
