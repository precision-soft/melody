package category

import (
    nethttp "net/http"
    "testing"

    "github.com/precision-soft/melody/v4/.example/entity"
)

func TestApiUpdateHandler_RenamesTheCategory(t *testing.T) {
    fixture := newCategoryDoorFixture(t)

    status, body := fixture.call(t, ApiUpdateHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodPut, `{"name":"Memory modules"}`, map[string]string{"id": "cat-1"})
    if nethttp.StatusOK != status {
        t.Fatalf("expected 200, got %d with body %q", status, body)
    }

    envelope := categoryEnvelope{}
    decodeBody(t, body, &envelope)

    if "cat-1" != envelope.Payload.Id || "Memory modules" != envelope.Payload.Name {
        t.Fatalf("expected the renamed category in the payload, got %+v", envelope.Payload)
    }

    if stored, _ := fixture.stored(t, "cat-1"); "Memory modules" != stored.Name {
        t.Fatalf("expected the new name stored, got %+v", stored)
    }
}

func TestApiUpdateHandler_AnswersNotFound(t *testing.T) {
    fixture := newCategoryDoorFixture(t)

    status, body := fixture.call(t, ApiUpdateHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodPut, `{"name":"Nothing"}`, map[string]string{"id": "cat-absent"})
    if nethttp.StatusNotFound != status {
        t.Fatalf("expected 404, got %d with body %q", status, body)
    }
}

func TestApiUpdateHandler_TrimsThePathIdentifier(t *testing.T) {
    fixture := newCategoryDoorFixture(t)

    status, body := fixture.call(t, ApiUpdateHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodPut, `{"name":"Memory modules"}`, map[string]string{"id": " cat-1 "})
    if nethttp.StatusOK != status {
        t.Fatalf("expected 200 under a padded identifier, got %d with body %q", status, body)
    }

    if stored, _ := fixture.stored(t, "cat-1"); "Memory modules" != stored.Name {
        t.Fatalf("expected the category under the trimmed identifier renamed, got %+v", stored)
    }
}

func TestApiUpdateHandler_RefusesACallerWithoutTheRole(t *testing.T) {
    fixture := newCategoryDoorFixture(t)

    status, body := fixture.call(t, ApiUpdateHandler(), fixture.runtimeFor(entity.RoleUser), nethttp.MethodPut, `{"name":"Memory modules"}`, map[string]string{"id": "cat-1"})
    if nethttp.StatusForbidden != status {
        t.Fatalf("expected 403, got %d with body %q", status, body)
    }

    if stored, _ := fixture.stored(t, "cat-1"); "Memory" != stored.Name {
        t.Fatalf("a refused update renamed the category: %+v", stored)
    }
}
