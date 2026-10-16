package currency

import (
    nethttp "net/http"
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
)

func TestApiUpdateDoorRefusesAUserWithoutTheEditorRole(t *testing.T) {
    fixture := newCurrencyDoorFixture(t)

    status, body := fixture.call(t, ApiUpdateHandler(), fixture.runtimeFor(entity.RoleUser), nethttp.MethodPut, `{"code":"EUR","name":"Renamed"}`, map[string]string{"id": "cur-eur"})
    if nethttp.StatusForbidden != status {
        t.Fatalf("expected 403, got %d with body %q", status, body)
    }

    if stored, _ := fixture.stored(t, "cur-eur"); "Euro" != stored.Name {
        t.Fatalf("a refused update renamed the currency to %q", stored.Name)
    }
}

func TestApiUpdateDoorRefusesAMissingId(t *testing.T) {
    fixture := newCurrencyDoorFixture(t)

    status, body := fixture.call(t, ApiUpdateHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodPut, `{"code":"EUR","name":"Renamed"}`, map[string]string{"id": " "})
    if nethttp.StatusBadRequest != status {
        t.Fatalf("expected 400, got %d with body %q", status, body)
    }
}

func TestApiUpdateDoorAnswersNotFoundForAnAbsentCurrency(t *testing.T) {
    fixture := newCurrencyDoorFixture(t)

    status, body := fixture.call(t, ApiUpdateHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodPut, `{"code":"XXX","name":"Absent"}`, map[string]string{"id": "cur-absent"})
    if nethttp.StatusNotFound != status {
        t.Fatalf("expected 404, got %d with body %q", status, body)
    }
}

func TestApiUpdateDoorRenamesTheCurrencyAndKeepsItsRate(t *testing.T) {
    fixture := newCurrencyDoorFixture(t)

    before, _ := fixture.stored(t, "cur-usd")

    status, body := fixture.call(t, ApiUpdateHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodPut, `{"code":"USD","name":"United States Dollar"}`, map[string]string{"id": "cur-usd"})
    if nethttp.StatusOK != status {
        t.Fatalf("expected 200, got %d with body %q", status, body)
    }

    envelope := currencyEnvelope{}
    decodeBody(t, body, &envelope)

    if "United States Dollar" != envelope.Payload.Name || before.Rate != envelope.Payload.Rate {
        t.Fatalf("expected the renamed currency with its rate, got %+v", envelope.Payload)
    }

    if stored, _ := fixture.stored(t, "cur-usd"); "United States Dollar" != stored.Name || before.Rate != stored.Rate {
        t.Fatalf("expected the rename in the repository and the rate kept, got %+v", stored)
    }
}

func TestApiUpdateDoorAnswersACodeAnotherCurrencyHoldsAsAConflict(t *testing.T) {
    fixture := newCurrencyDoorFixture(t)

    status, body := fixture.call(t, ApiUpdateHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodPut, `{"code":"EUR","name":"Not the Euro"}`, map[string]string{"id": "cur-usd"})
    if nethttp.StatusConflict != status {
        t.Fatalf("expected 409 for a rename onto a taken code, got %d with body %q", status, body)
    }

    if stored, _ := fixture.stored(t, "cur-usd"); "USD" != stored.Code {
        t.Fatalf("a refused rename changed the code to %q", stored.Code)
    }

    status, body = fixture.call(t, ApiUpdateHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodPut, `{"code":"EUR","name":"Euro"}`, map[string]string{"id": "cur-eur"})
    if nethttp.StatusOK != status {
        t.Fatalf("expected a currency to keep its own code, got %d with body %q", status, body)
    }
}

func TestApiUpdateDoorValidatesTheTrimmedBody(t *testing.T) {
    fixture := newCurrencyDoorFixture(t)

    status, body := fixture.call(t, ApiUpdateHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodPut, `{"code":"USD","name":"X "}`, map[string]string{"id": "cur-usd"})
    if nethttp.StatusBadRequest != status {
        t.Fatalf("expected 400 for a name that is one rune once trimmed, got %d with body %q", status, body)
    }
}
