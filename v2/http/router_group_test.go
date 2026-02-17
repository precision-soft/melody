package http

import (
	nethttp "net/http"
	"testing"

	httpcontract "github.com/precision-soft/melody/v2/http/contract"
	runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

func TestRouteGroup_PanicsWhenRouterIsNil(t *testing.T) {
	group := NewRouteGroup(nil, "/api")

	defer func() {
		if nil == recover() {
			t.Fatalf("expected panic")
		}
	}()

	group.HandleWithOptions(
		"/x",
		func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
			return EmptyResponse(200), nil
		},
		NewRouteOptions("a", []string{nethttp.MethodGet}, "", nil, nil, nil, nil, 0, nil),
	)
}

func TestRouteGroup_PanicsWhenOptionsIsNil(t *testing.T) {
	router := NewRouter()
	group := router.Group("/api")

	defer func() {
		if nil == recover() {
			t.Fatalf("expected panic")
		}
	}()

	group.HandleWithOptions(
		"/x",
		func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
			return EmptyResponse(200), nil
		},
		nil,
	)
}

func TestRouteGroup_JoinsPathAndPrefixesName(t *testing.T) {
	routeRegistry := NewRouteRegistry()
	router := NewRouterWithRouteRegistry(routeRegistry)
	urlGenerator := NewUrlGenerator(routeRegistry)
	group := router.Group("/api")

	group.WithNamePrefix("api.")

	defer func() {
		if nil != recover() {
			t.Fatalf("unexpected panic")
		}
	}()

	group.HandleWithOptions(
		"/user/:id",
		func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
			return EmptyResponse(200), nil
		},
		NewRouteOptions("user_show", []string{nethttp.MethodGet}, "", nil, nil, nil, nil, 0, nil),
	)

	pathValue, err := urlGenerator.GeneratePath("api.user_show", map[string]string{"id": "1"})
	if nil != err {
		t.Fatalf("unexpected error: %v", err)
	}

	if "/api/user/1" != pathValue {
		t.Fatalf("unexpected path")
	}
}

func TestRouteGroup_MergesDefaultsAndDoesNotOverrideRouteDefault(t *testing.T) {
	routeRegistry := NewRouteRegistry()
	router := NewRouterWithRouteRegistry(routeRegistry)
	urlGenerator := NewUrlGenerator(routeRegistry)
	group := router.Group("/api")

	group.WithDefaults(
		map[string]string{
			"page":   "1",
			"locale": "en",
		},
	)

	defer func() {
		if nil != recover() {
			t.Fatalf("unexpected panic")
		}
	}()

	group.HandleWithOptions(
		"/:locale?/list/:page",
		func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
			return EmptyResponse(200), nil
		},
		NewRouteOptions(
			"list",
			[]string{nethttp.MethodGet},
			"",
			nil,
			nil,
			map[string]string{
				"page": "2",
			},
			nil,
			0,
			nil,
		),
	)

	pathValue, err := urlGenerator.GeneratePath("list", map[string]string{})
	if nil != err {
		t.Fatalf("unexpected error: %v", err)
	}

	if "/api/en/list/2" != pathValue {
		t.Fatalf("unexpected path")
	}
}

func TestRouteGroup_MergesRequirementsAndDoesNotOverrideRouteRequirement(t *testing.T) {
	routeRegistry := NewRouteRegistry()
	router := NewRouterWithRouteRegistry(routeRegistry)
	urlGenerator := NewUrlGenerator(routeRegistry)
	group := router.Group("/api")

	group.WithRequirements(
		map[string]string{
			"id": "\\d+",
		},
	)

	defer func() {
		if nil != recover() {
			t.Fatalf("unexpected panic")
		}
	}()

	group.HandleWithOptions(
		"/user/:id",
		func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
			return EmptyResponse(200), nil
		},
		NewRouteOptions(
			"user",
			[]string{nethttp.MethodGet},
			"",
			nil,
			map[string]string{
				"id": "[a-z]+",
			},
			nil,
			nil,
			0,
			nil,
		),
	)

	_, err := urlGenerator.GeneratePath("user", map[string]string{"id": "abc"})
	if nil != err {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = urlGenerator.GeneratePath("user", map[string]string{"id": "123"})
	if nil == err {
		t.Fatalf("expected error")
	}
}
