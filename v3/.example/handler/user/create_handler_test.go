package user

import (
    "context"
    "errors"
    "fmt"
    nethttp "net/http"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/security"
)

/* the create door holds the same line the update door holds: a role carrying a comma would come back as several roles on the next read, among them an administrator nobody granted, and an account that reads as an administrator is then shielded from every other administrator by protectsAnotherAdmin */
func TestApiCreateHandlerRefusesARoleCarryingAComma(t *testing.T) {
    userRepository := newRecordingUserRepository(administrator("admin-1"))
    runtimeInstance := adminRuntime(t, userRepository, "admin-1", []string{entity.RoleAdmin})

    statusCode, body := callDoor(
        t,
        runtimeInstance,
        ApiCreateHandler(),
        nethttp.MethodPost,
        "/users/api/create/",
        nil,
        `{"username":"smuggled","password":"a-password","roles":["ROLE_X,`+entity.RoleAdmin+`"]}`,
    )

    if nethttp.StatusBadRequest != statusCode {
        t.Fatalf("the smuggled role answered %d: %s", statusCode, body)
    }

    if false == strings.Contains(body, "must not contain commas") {
        t.Fatalf("the refusal did not name the reason: %s", body)
    }

    if _, exists, _ := userRepository.FindByUsername(context.Background(), "smuggled"); true == exists {
        t.Fatal("the refused account reached the directory anyway")
    }
}

/* a username longer than the 255-byte column, and than the cache key grammar admits once escaped, is turned away at the door as the caller's mistake instead of landing as a driver error, or as a row the lookup doors could never ask the cache about */
func TestApiCreateHandlerRefusesAUsernameTheTableCannotHold(t *testing.T) {
    userRepository := newRecordingUserRepository(administrator("admin-1"))
    runtimeInstance := adminRuntime(t, userRepository, "admin-1", []string{entity.RoleAdmin})

    longUsername := strings.Repeat("a", 256)

    statusCode, body := callDoor(
        t,
        runtimeInstance,
        ApiCreateHandler(),
        nethttp.MethodPost,
        "/users/api/create/",
        nil,
        `{"username":"`+longUsername+`","password":"a-password","roles":["`+entity.RoleUser+`"]}`,
    )

    if nethttp.StatusBadRequest != statusCode {
        t.Fatalf("the oversized username answered %d: %s", statusCode, body)
    }

    if false == strings.Contains(body, "within 255 bytes") {
        t.Fatalf("the refusal did not name the reason: %s", body)
    }

    if _, exists, _ := userRepository.FindByUsername(context.Background(), longUsername); true == exists {
        t.Fatal("the refused account reached the directory anyway")
    }
}

/* the same door accepts a plain role list, so the refusal above is the comma and not the door */
func TestApiCreateHandlerAcceptsAPlainRoleList(t *testing.T) {
    userRepository := newRecordingUserRepository(administrator("admin-1"))
    runtimeInstance := adminRuntime(t, userRepository, "admin-1", []string{entity.RoleAdmin})

    statusCode, body := callDoor(
        t,
        runtimeInstance,
        ApiCreateHandler(),
        nethttp.MethodPost,
        "/users/api/create/",
        nil,
        `{"username":"plain","password":"a-password","roles":["`+entity.RoleUser+`","`+entity.RoleEditor+`"]}`,
    )

    if nethttp.StatusCreated != statusCode {
        t.Fatalf("a plain role list answered %d: %s", statusCode, body)
    }

    created, exists, err := userRepository.FindByUsername(context.Background(), "plain")
    if nil != err || false == exists {
        t.Fatalf("the accepted account did not reach the directory: %v", err)
    }

    if 2 != len(created.Roles) || entity.RoleUser != created.Roles[0] || entity.RoleEditor != created.Roles[1] {
        t.Fatalf("unexpected roles stored: %v", created.Roles)
    }
}

/* the check that precedes the write is a read, so two callers can pass it before either has written; the
   unique index refuses the second, and its refusal — wrapped by the audit tracker, recognised by the
   repository's seam — is the caller's 400, the same answer the check gives, not a 500 of a failed write */
func TestApiCreateHandlerAnswersTheIndexsRefusalOfATakenUsernameAs400(t *testing.T) {
    userRepository := newRecordingUserRepository(administrator("admin-1"))
    userRepository.refuseWrites(fmt.Errorf("audited insert failed: %w", repository.ErrUsernameAlreadyExists))
    runtimeInstance := adminRuntime(t, userRepository, "admin-1", []string{entity.RoleAdmin})

    statusCode, body := callDoor(
        t,
        runtimeInstance,
        ApiCreateHandler(),
        nethttp.MethodPost,
        "/users/api/create/",
        nil,
        `{"username":"taken","password":"a-password","roles":["`+entity.RoleUser+`"]}`,
    )

    if nethttp.StatusBadRequest != statusCode || false == strings.Contains(body, "username already exists") {
        t.Fatalf("the index's refusal answered %d: %s, wanted 400 username already exists", statusCode, body)
    }
}

func TestApiCreateHandlerAnswersAnyOtherFailedWriteAs500(t *testing.T) {
    userRepository := newRecordingUserRepository(administrator("admin-1"))
    userRepository.refuseWrites(errors.New("audited insert failed"))
    runtimeInstance := adminRuntime(t, userRepository, "admin-1", []string{entity.RoleAdmin})

    statusCode, _ := callDoor(
        t,
        runtimeInstance,
        ApiCreateHandler(),
        nethttp.MethodPost,
        "/users/api/create/",
        nil,
        `{"username":"plain","password":"a-password","roles":["`+entity.RoleUser+`"]}`,
    )

    if nethttp.StatusInternalServerError != statusCode {
        t.Fatalf("a failed write answered %d, wanted 500", statusCode)
    }
}

/* the voter compares a role's spelling exactly, so a spelling outside the closed vocabulary would be stored,
   reported created and grant nothing: the door refuses it, naming the vocabulary, the way the console grant does */
func TestApiCreateHandlerRefusesARoleTheApplicationDoesNotKnow(t *testing.T) {
    userRepository := newRecordingUserRepository(administrator("admin-1"))
    runtimeInstance := adminRuntime(t, userRepository, "admin-1", []string{entity.RoleAdmin})

    statusCode, body := callDoor(
        t,
        runtimeInstance,
        ApiCreateHandler(),
        nethttp.MethodPost,
        "/users/api/create/",
        nil,
        `{"username":"misspelt","password":"a-password","roles":["ROLE_ADMIM"]}`,
    )

    if nethttp.StatusBadRequest != statusCode || false == strings.Contains(body, "ROLE_ADMIM is not one this application knows") {
        t.Fatalf("the misspelt role answered %d: %s", statusCode, body)
    }

    if _, exists, _ := userRepository.FindByUsername(context.Background(), "misspelt"); true == exists {
        t.Fatal("the refused account was created anyway")
    }
}

/* bcrypt reads 72 bytes of the plaintext: the 72nd is the last a sign-in can tell apart, so the door takes it and refuses the 73rd */
func TestApiCreateDoor_AcceptsA72BytePasswordAndRefuses73(t *testing.T) {
    userRepository := newRecordingUserRepository(administrator("admin-1"))
    runtimeInstance := adminRuntime(t, userRepository, "admin-1", []string{entity.RoleAdmin})

    statusCode, body := callDoor(t, runtimeInstance, ApiCreateHandler(), nethttp.MethodPost, "/users/api/create/", nil,
        `{"username":"refused-73","password":"`+strings.Repeat("p", security.PasswordMaximumBytes+1)+`","roles":["`+entity.RoleUser+`"]}`)
    if nethttp.StatusBadRequest != statusCode || false == strings.Contains(body, security.PasswordTooLongMessage) {
        t.Fatalf("the 73-byte password answered %d: %s", statusCode, body)
    }

    statusCode, body = callDoor(t, runtimeInstance, ApiCreateHandler(), nethttp.MethodPost, "/users/api/create/", nil,
        `{"username":"accepted-72","password":"`+strings.Repeat("p", security.PasswordMaximumBytes)+`","roles":["`+entity.RoleUser+`"]}`)
    if nethttp.StatusCreated != statusCode {
        t.Fatalf("the 72-byte password answered %d: %s", statusCode, body)
    }

    if _, exists, _ := userRepository.FindByUsername(context.Background(), "refused-73"); true == exists {
        t.Fatal("the refused account reached the directory anyway")
    }
}

func TestApiCreateDoor_AcceptsA255ByteUsername(t *testing.T) {
    userRepository := newRecordingUserRepository(administrator("admin-1"))
    runtimeInstance := adminRuntime(t, userRepository, "admin-1", []string{entity.RoleAdmin})

    username := strings.Repeat("a", 255)
    statusCode, body := callDoor(t, runtimeInstance, ApiCreateHandler(), nethttp.MethodPost, "/users/api/create/", nil,
        `{"username":"`+username+`","password":"a-password","roles":["`+entity.RoleUser+`"]}`)
    if nethttp.StatusCreated != statusCode {
        t.Fatalf("the 255-byte username answered %d: %s", statusCode, body)
    }

    if _, exists, _ := userRepository.FindByUsername(context.Background(), username); false == exists {
        t.Fatal("the accepted account is not in the directory")
    }
}

/* the kernel bounds every body; a body past it is the client's too large a payload, not unreadable json */
func TestApiCreateDoor_AnswersABodyPastTheLimit413(t *testing.T) {
    userRepository := newRecordingUserRepository(administrator("admin-1"), editor("editor-1"))
    runtimeInstance := adminRuntime(t, userRepository, "admin-1", []string{entity.RoleAdmin})

    statusCode, body := callDoorBoundedTo(t, runtimeInstance, ApiCreateHandler(), nethttp.MethodPost, "/users/api/create/", nil, `{"username":"bounded-account","password":"a-password"}`, 8)
    if nethttp.StatusRequestEntityTooLarge != statusCode {
        t.Fatalf("expected 413, got %d: %s", statusCode, body)
    }

    if _, exists, _ := userRepository.FindByUsername(context.Background(), "bounded-account"); true == exists {
        t.Fatal("expected nothing stored from a body never read")
    }
}
