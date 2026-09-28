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

func TestApiDeleteDoorDeletesTheCurrency(t *testing.T) {
    fixture := newCurrencyDoorFixture(t)

    status, body := fixture.call(t, ApiDeleteHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodDelete, "", map[string]string{"id": "cur-ron"})
    if nethttp.StatusOK != status {
        t.Fatalf("expected 200, got %d with body %q", status, body)
    }

    if _, found := fixture.stored(t, "cur-ron"); true == found {
        t.Fatalf("expected the currency gone from the repository")
    }
}
