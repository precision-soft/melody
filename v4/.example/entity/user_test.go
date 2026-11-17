package entity

import (
    "reflect"
    "testing"
)

func TestUser_ProfileCopiesTheRolesAndLeavesThePasswordOut(t *testing.T) {
    user := NewUser("user-1", "user", "digest", []string{RoleUser, RoleEditor})

    profile := user.Profile()
    if "user-1" != profile.Id || "user" != profile.Username || false == reflect.DeepEqual([]string{RoleUser, RoleEditor}, profile.Roles) {
        t.Fatalf("expected the profile of the account, got %+v", profile)
    }

    profile.Roles[0] = RoleAdmin
    if RoleUser != user.Roles[0] {
        t.Fatalf("expected the account's roles untouched by the profile, got %v", user.Roles)
    }

    for index := 0; index < reflect.TypeOf(*profile).NumField(); index++ {
        if fieldName := reflect.TypeOf(*profile).Field(index).Name; "Id" != fieldName && "Username" != fieldName && "Roles" != fieldName {
            t.Fatalf("expected the profile to hold the id, the name and the roles alone, found %s", fieldName)
        }
    }
}
