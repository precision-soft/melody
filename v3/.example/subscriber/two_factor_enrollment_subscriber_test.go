package subscriber

import (
    "strings"
    "testing"
    "github.com/precision-soft/melody/v3/.example/event"
)

func TestTwoFactorEnrollmentSubscriber_ADeletedAccountReleasesItsEnrollment(t *testing.T) {
    statements := deleteEnrollmentStatementsAfter(t, event.UserDeletedEventName, event.NewUserDeletedEvent("user-4", "dave"))

    if 1 != len(statements) {
        t.Fatalf("expected one statement for the deleted account, got %d: %v", len(statements), statements)
    }

    statement := statements[0]
    if false == strings.HasPrefix(statement, "DELETE FROM `melody_example_v3_two_factor`") || false == strings.Contains(statement, "WHERE (user_identifier = 'user-4')") {
        t.Fatalf("expected the enrollment of user-4 alone to be deleted, got %q", statement)
    }
}

func TestTwoFactorEnrollmentSubscriber_APayloadThatIsNotADeletionReleasesNothing(t *testing.T) {
    if statements := deleteEnrollmentStatementsAfter(t, event.UserDeletedEventName, "user-4"); 0 != len(statements) {
        t.Fatalf("expected no statement for a payload that is not a deletion, got %v", statements)
    }

    var absent *event.UserDeletedEvent
    if statements := deleteEnrollmentStatementsAfter(t, event.UserDeletedEventName, absent); 0 != len(statements) {
        t.Fatalf("expected no statement for a nil deletion, got %v", statements)
    }
}
