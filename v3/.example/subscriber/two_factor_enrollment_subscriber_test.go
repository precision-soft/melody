package subscriber

import (
    "context"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/.example/event"
    "github.com/precision-soft/melody/v3/.example/twofactor"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyevent "github.com/precision-soft/melody/v3/event"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
)

func deleteEnrollmentStatementsAfter(t *testing.T, eventName string, payload any) []string {
    t.Helper()

    database, recorder := newRecordingDatabase()
    subscriberInstance := NewTwoFactorEnrollmentSubscriber(twofactor.NewStore(database))

    containerInstance := melodycontainer.NewContainer()
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    listeners := subscriberInstance.SubscribedEvents()[event.UserDeletedEventName]
    if 1 != len(listeners) {
        t.Fatalf("expected one listener on %s, got %d", event.UserDeletedEventName, len(listeners))
    }

    if listenErr := listeners[0].Listener()(runtimeInstance, melodyevent.NewEvent(eventName, payload, melodyclock.NewSystemClock())); nil != listenErr {
        t.Fatalf("expected the listener to answer nil, got %v", listenErr)
    }

    return recorder.recordedQueries()
}

/* the enrollment is keyed on the account identifier and the example mints identifiers as the highest suffix plus one, so a row that outlived its account started the next holder of the identifier enrolled with the previous holder's secret and recovery codes: the deletion the account publishes releases the factor, keyed on the identifier the event carries and on nothing wider. */
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

/* a payload that is not a deletion — or none at all — releases nothing: the listener is registered on the deletion name and still reads the payload it is handed, the way every subscriber of the example does. */
func TestTwoFactorEnrollmentSubscriber_APayloadThatIsNotADeletionReleasesNothing(t *testing.T) {
    if statements := deleteEnrollmentStatementsAfter(t, event.UserDeletedEventName, "user-4"); 0 != len(statements) {
        t.Fatalf("expected no statement for a payload that is not a deletion, got %v", statements)
    }

    var absent *event.UserDeletedEvent
    if statements := deleteEnrollmentStatementsAfter(t, event.UserDeletedEventName, absent); 0 != len(statements) {
        t.Fatalf("expected no statement for a nil deletion, got %v", statements)
    }
}
