package category

import (
    "context"
    "encoding/json"
    nethttp "net/http"
    "testing"

    "github.com/precision-soft/melody/v4/.example/entity"
)

func TestApiReadAllHandler_ReadsTheCachedList(t *testing.T) {
    fixture := newCategoryDoorFixture(t)

    countOf := func() int {
        status, body := fixture.call(t, ApiReadAllHandler(), fixture.runtimeFor(entity.RoleUser), nethttp.MethodGet, "", nil)
        if nethttp.StatusOK != status {
            t.Fatalf("expected 200, got %d with body %q", status, body)
        }

        envelope := struct {
            Payload []CategoryResponse `json:"payload"`
        }{}
        if decodeErr := json.Unmarshal([]byte(body), &envelope); nil != decodeErr {
            t.Fatalf("decode body %q: %v", body, decodeErr)
        }

        return len(envelope.Payload)
    }

    first := countOf()

    if createErr := fixture.categoryRepository.Create(context.Background(), entity.NewCategory("cat-past", "Past the cache")); nil != createErr {
        t.Fatalf("create past the service: %v", createErr)
    }

    if second := countOf(); first != second {
        t.Fatalf("expected the cached listing of %d rows, got %d", first, second)
    }
}
