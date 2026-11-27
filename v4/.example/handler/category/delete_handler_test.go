package category

import (
    nethttp "net/http"
    "testing"

    "github.com/precision-soft/melody/v4/.example/entity"
)

func TestApiDeleteHandler_DeletesTheCategory(t *testing.T) {
    fixture := newCategoryDoorFixture(t)

    status, body := fixture.call(t, ApiDeleteHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodDelete, "", map[string]string{"id": " cat-4 "})
    if nethttp.StatusOK != status {
        t.Fatalf("expected 200, got %d with body %q", status, body)
    }

    if _, found := fixture.stored(t, "cat-4"); true == found {
        t.Fatalf("expected the category deleted")
    }
}

func TestApiDeleteHandler_AnswersConflictWhileAProductNamesIt(t *testing.T) {
    fixture := newCategoryDoorFixture(t)

    status, body := fixture.call(t, ApiDeleteHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodDelete, "", map[string]string{"id": "cat-1"})
    if nethttp.StatusConflict != status {
        t.Fatalf("expected 409, got %d with body %q", status, body)
    }

    envelope := categoryEnvelope{}
    decodeBody(t, body, &envelope)

    if 1 != len(envelope.Errors) || "category is in use" != envelope.Errors[0] {
        t.Fatalf("expected the refusal to say the category is in use, got %v", envelope.Errors)
    }

    if _, found := fixture.stored(t, "cat-1"); false == found {
        t.Fatalf("a refused delete removed the category")
    }
}

func TestApiDeleteHandler_AnswersNotFound(t *testing.T) {
    fixture := newCategoryDoorFixture(t)

    status, body := fixture.call(t, ApiDeleteHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodDelete, "", map[string]string{"id": "cat-absent"})
    if nethttp.StatusNotFound != status {
        t.Fatalf("expected 404, got %d with body %q", status, body)
    }
}

func TestApiDeleteHandler_RefusesACallerWithoutTheRole(t *testing.T) {
    fixture := newCategoryDoorFixture(t)

    status, body := fixture.call(t, ApiDeleteHandler(), fixture.runtimeFor(entity.RoleUser), nethttp.MethodDelete, "", map[string]string{"id": "cat-4"})
    if nethttp.StatusForbidden != status {
        t.Fatalf("expected 403, got %d with body %q", status, body)
    }

    if _, found := fixture.stored(t, "cat-4"); false == found {
        t.Fatalf("a refused delete removed the category")
    }
}

/* the in-use check compares the identifier as given and the in-memory delete trims it, so only the trimmed identifier keeps a padded one from deleting a category a product sits in */
func TestApiDeleteHandler_TrimsThePathIdentifier(t *testing.T) {
    fixture := newCategoryDoorFixture(t)

    status, body := fixture.call(t, ApiDeleteHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodDelete, "", map[string]string{"id": " cat-1 "})
    if nethttp.StatusConflict != status {
        t.Fatalf("expected 409 under a padded identifier, got %d with body %q", status, body)
    }

    if _, found := fixture.stored(t, "cat-1"); false == found {
        t.Fatalf("a padded identifier deleted a category a product sits in")
    }
}
