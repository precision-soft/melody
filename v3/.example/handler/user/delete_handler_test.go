package user

import (
    nethttp "net/http"
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/service"
)

func TestApiDeleteHandlerRefusesAPeerAdministratorAndRemovesNothing(t *testing.T) {
    userRepository := newRecordingUserRepository(administrator("admin-1"), administrator("admin-2"))
    runtimeInstance := adminRuntime(t, userRepository, "admin-1", []string{entity.RoleAdmin})

    statusCode, body := callDoor(t, runtimeInstance, ApiDeleteHandler(), nethttp.MethodDelete, "/users/api/delete/admin-2/", map[string]string{"id": "admin-2"}, "")
    if nethttp.StatusForbidden != statusCode {
        t.Fatalf("a delete of a peer administrator answered %d: %s", statusCode, body)
    }

    userRepository.stored(t, "admin-2")

    statusCode, body = callDoor(t, runtimeInstance, ApiDeleteHandler(), nethttp.MethodDelete, "/users/api/delete/admin-1/", map[string]string{"id": "admin-1"}, "")
    if nethttp.StatusOK != statusCode {
        t.Fatalf("an administrator's delete of their own account answered %d: %s", statusCode, body)
    }
}

/* the peer decision is taken on the directory: an account the cache still holds as an editor, promoted behind it, is refused */
func TestApiDeleteHandlerDecidesThePeerRefusalOnTheDirectoryNotTheCache(t *testing.T) {
    userRepository := newRecordingUserRepository(administrator("admin-1"), administrator("admin-2"))
    cacheInstance := &valueCache{values: map[string]any{service.CacheKeyUserById("admin-2"): editor("admin-2")}}
    runtimeInstance := adminRuntimeOverCache(t, userRepository, cacheInstance, "admin-1", []string{entity.RoleAdmin})

    statusCode, body := callDoor(t, runtimeInstance, ApiDeleteHandler(), nethttp.MethodDelete, "/users/api/delete/admin-2/", map[string]string{"id": "admin-2"}, "")
    if nethttp.StatusForbidden != statusCode {
        t.Fatalf("a promoted peer the cache still holds as an editor answered %d: %s", statusCode, body)
    }

    userRepository.stored(t, "admin-2")
}
