package config

import (
    "testing"
)

func TestRoleAllowsBackgroundWork(t *testing.T) {
    if false == RoleAllowsBackgroundWork(RoleWorker) {
        t.Fatalf("expected the worker role to allow background work")
    }

    if false == RoleAllowsBackgroundWork(RoleAll) {
        t.Fatalf("expected the all role to allow background work")
    }

    if true == RoleAllowsBackgroundWork(RoleWeb) {
        t.Fatalf("expected the web role to not allow background work")
    }

    if true == RoleAllowsBackgroundWork("") {
        t.Fatalf("expected an empty role to not allow background work")
    }
}

func TestRoleAllowsHttp(t *testing.T) {
    if false == RoleAllowsHttp(RoleWeb) {
        t.Fatalf("expected the web role to allow http")
    }

    if false == RoleAllowsHttp(RoleAll) {
        t.Fatalf("expected the all role to allow http")
    }

    if true == RoleAllowsHttp(RoleWorker) {
        t.Fatalf("expected the worker role to not allow http")
    }
}
