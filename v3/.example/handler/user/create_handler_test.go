package user

import (
    "context"
    nethttp "net/http"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
)

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
