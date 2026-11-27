package category

import (
    nethttp "net/http"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/.example/entity"
)

func TestApiCreateHandler_CreatesAndAnswersTheCategory(t *testing.T) {
    fixture := newCategoryDoorFixture(t)

    status, body := fixture.call(t, ApiCreateHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodPost, `{"id":"cat-books","name":" Books "}`, nil)
    if nethttp.StatusCreated != status {
        t.Fatalf("expected 201, got %d with body %q", status, body)
    }

    envelope := categoryEnvelope{}
    decodeBody(t, body, &envelope)

    if "cat-books" != envelope.Payload.Id || "Books" != envelope.Payload.Name {
        t.Fatalf("expected the created category in the payload, got %+v", envelope.Payload)
    }

    stored, found := fixture.stored(t, "cat-books")
    if false == found || "Books" != stored.Name {
        t.Fatalf("expected the trimmed category in the repository, got %+v found=%v", stored, found)
    }
}

func TestApiCreateHandler_RefusesABlankName(t *testing.T) {
    fixture := newCategoryDoorFixture(t)

    status, body := fixture.call(t, ApiCreateHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodPost, `{"id":"cat-blank","name":"   "}`, nil)
    if nethttp.StatusBadRequest != status {
        t.Fatalf("expected 400, got %d with body %q", status, body)
    }

    envelope := categoryEnvelope{}
    decodeBody(t, body, &envelope)

    if 0 == len(envelope.Errors) || false == strings.HasPrefix(strings.Join(envelope.Errors, "\n"), "name: ") {
        t.Fatalf("expected the refusal to name the field, got %v", envelope.Errors)
    }

    if _, found := fixture.stored(t, "cat-blank"); true == found {
        t.Fatalf("a refused create wrote the category")
    }
}

func TestApiCreateHandler_RefusesTrailingData(t *testing.T) {
    fixture := newCategoryDoorFixture(t)

    status, body := fixture.call(t, ApiCreateHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodPost, `{"id":"cat-trail","name":"Trail"}{"id":"cat-other","name":"Other"}`, nil)
    if nethttp.StatusBadRequest != status {
        t.Fatalf("expected 400 for a body carrying a second document, got %d with body %q", status, body)
    }

    if _, found := fixture.stored(t, "cat-trail"); true == found {
        t.Fatalf("a refused create wrote the category")
    }
}

func TestApiCreateHandler_AnswersConflictForAHeldIdentifier(t *testing.T) {
    fixture := newCategoryDoorFixture(t)

    status, body := fixture.call(t, ApiCreateHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodPost, `{"id":"cat-1","name":"Second"}`, nil)
    if nethttp.StatusConflict != status {
        t.Fatalf("expected 409 for a held identifier, got %d with body %q", status, body)
    }

    envelope := categoryEnvelope{}
    decodeBody(t, body, &envelope)

    if 1 != len(envelope.Errors) || "id already exists" != envelope.Errors[0] {
        t.Fatalf("expected the refusal to name the identifier, got %v", envelope.Errors)
    }

    if stored, _ := fixture.stored(t, "cat-1"); "Memory" != stored.Name {
        t.Fatalf("expected the held category kept, got %+v", stored)
    }
}

func TestApiCreateHandler_RefusesAnIdentifierAtTheCeiling(t *testing.T) {
    fixture := newCategoryDoorFixture(t)

    status, body := fixture.call(t, ApiCreateHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodPost, `{"id":"cat-9223372036854775807","name":"Ceiling"}`, nil)
    if nethttp.StatusBadRequest != status {
        t.Fatalf("expected 400 for an identifier at the ceiling, got %d with body %q", status, body)
    }

    envelope := categoryEnvelope{}
    decodeBody(t, body, &envelope)

    if 1 != len(envelope.Errors) || false == strings.HasPrefix(envelope.Errors[0], "id: ") {
        t.Fatalf("expected the refusal to name the identifier, got %v", envelope.Errors)
    }
}

func TestApiCreateHandler_RefusesACallerWithoutTheRole(t *testing.T) {
    fixture := newCategoryDoorFixture(t)

    status, body := fixture.call(t, ApiCreateHandler(), fixture.runtimeFor(entity.RoleUser), nethttp.MethodPost, `{"id":"cat-books","name":"Books"}`, nil)
    if nethttp.StatusForbidden != status {
        t.Fatalf("expected 403, got %d with body %q", status, body)
    }

    if _, found := fixture.stored(t, "cat-books"); true == found {
        t.Fatalf("a refused create wrote the category")
    }
}
