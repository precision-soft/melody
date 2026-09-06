package user

import (
    "context"
    nethttp "net/http"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
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
