package currency

import (
    nethttp "net/http"
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
)

func TestApiDeleteDoorRefusesAUserWithoutTheEditorRole(t *testing.T) {
    fixture := newCurrencyDoorFixture(t)

    status, body := fixture.call(t, ApiDeleteHandler(), fixture.runtimeFor(entity.RoleUser), nethttp.MethodDelete, "", map[string]string{"id": "cur-ron"})
    if nethttp.StatusForbidden != status {
        t.Fatalf("expected 403, got %d with body %q", status, body)
    }

    if _, found := fixture.stored(t, "cur-ron"); false == found {
        t.Fatalf("a refused delete removed the currency")
    }
}

func TestApiDeleteDoorAnswersNotFoundForAnAbsentCurrency(t *testing.T) {
    fixture := newCurrencyDoorFixture(t)

    status, body := fixture.call(t, ApiDeleteHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodDelete, "", map[string]string{"id": "cur-absent"})
    if nethttp.StatusNotFound != status {
        t.Fatalf("expected 404, got %d with body %q", status, body)
    }
}

func TestApiDeleteDoorDeletesACurrencyNoProductIsPricedIn(t *testing.T) {
    fixture := newCurrencyDoorFixture(t)

    status, body := fixture.call(t, ApiCreateHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodPost, `{"id":"cur-chf","code":"CHF","name":"Swiss Franc","rate":0.94}`, nil)
    if nethttp.StatusCreated != status {
        t.Fatalf("expected the create to answer 201, got %d with body %q", status, body)
    }

    status, body = fixture.call(t, ApiDeleteHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodDelete, "", map[string]string{"id": "cur-chf"})
    if nethttp.StatusOK != status {
        t.Fatalf("expected 200, got %d with body %q", status, body)
    }

    if _, found := fixture.stored(t, "cur-chf"); true == found {
        t.Fatalf("expected the currency gone from the repository")
    }
}

/* a seeded product is priced in cur-ron, so its delete is refused and the currency stays: the conversion of that product keeps a currency to read */
func TestApiDeleteDoorRefusesACurrencyAProductIsPricedIn(t *testing.T) {
    fixture := newCurrencyDoorFixture(t)

    status, body := fixture.call(t, ApiDeleteHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodDelete, "", map[string]string{"id": "cur-ron"})
    if nethttp.StatusConflict != status {
        t.Fatalf("expected 409, got %d with body %q", status, body)
    }

    if _, found := fixture.stored(t, "cur-ron"); false == found {
        t.Fatalf("a refused delete removed the currency")
    }
}
