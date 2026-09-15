package repository

import (
    "fmt"
    driver "github.com/go-sql-driver/mysql"
    "github.com/precision-soft/melody/v3/exception"
    "strings"
    "testing"
    "github.com/precision-soft/melody/v3/.example/migration"
)

func TestUserByUsernameQuery_ComparesOnTheBinaryCollation(t *testing.T) {
    rendered := renderedUserByUsernameQuery(t, "café")

    if false == strings.Contains(rendered, "COLLATE utf8mb4_bin") {
        t.Fatalf(
            "expected the lookup to force the comparison onto the binary collation, so an accented spelling is a different name; got %q",
            rendered,
        )
    }

    if false == strings.Contains(rendered, "LOWER(username) = (") {
        t.Fatalf("expected the lookup to fold case the way NormalizedUsername does, got %q", rendered)
    }
}

func TestUsernameTakenByAnotherQuery_ComparesOnTheBinaryCollation(t *testing.T) {
    rendered := renderedUsernameTakenByAnotherQuery(t, "café", "user-1")

    if false == strings.Contains(rendered, "COLLATE utf8mb4_bin") {
        t.Fatalf(
            "expected the uniqueness door to admit and refuse exactly the spellings the lookup does; got %q",
            rendered,
        )
    }

    if false == strings.Contains(rendered, "id != ") {
        t.Fatalf("expected the uniqueness door to exclude the account being updated, got %q", rendered)
    }
}

func TestAsUsernameAlreadyExists_TranslatesOnlyTheUsernameIndexRefusal(t *testing.T) {
    refusal := fmt.Errorf(
        "audited insert failed: Error 1062 (23000): Duplicate entry 'zzprobe' for key 'melody_example_v3_user.%s'",
        migration.UserUsernameIndexName,
    )

    translated := asUsernameAlreadyExists(refusal)
    if nil == translated {
        t.Fatalf("expected the refusal to survive as an error")
    }

    if "username already exists" != translated.Error() {
        t.Fatalf("expected the door's own message, got %q", translated.Error())
    }
}

func TestAsUsernameAlreadyExists_LeavesEveryOtherFailureAlone(t *testing.T) {
    primaryKey := fmt.Errorf("audited insert failed: Error 1062 (23000): Duplicate entry 'user-7' for key 'melody_example_v3_user.PRIMARY'")

    if primaryKey != asUsernameAlreadyExists(primaryKey) {
        t.Fatalf("expected a duplicate identifier to stay the diagnosis it is, got %q", asUsernameAlreadyExists(primaryKey))
    }

    if nil != asUsernameAlreadyExists(nil) {
        t.Fatalf("expected a write that did not fail to stay unreported")
    }
}

func TestUsernameConflictSurvivesAuditErrorWrapping(t *testing.T) {
    for _, number := range []uint16{1062, 1146} {
        cause := &driver.MySQLError{Number: number, Message: "Duplicate entry for key '"+migration.UserUsernameIndexName+"'"}
        wrapped := exception.NewError("audited insert failed", nil, cause)
        result := asUsernameAlreadyExists(wrapped)
        if 1062 == number {
            if "username already exists" != result.Error() { t.Fatalf("lost username conflict inside audit wrapper: %v", result) }
        } else if wrapped != result { t.Fatalf("translated an unrelated driver error: %v", result) }
    }
}
