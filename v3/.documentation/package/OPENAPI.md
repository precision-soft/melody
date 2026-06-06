# OPENAPI

The [`openapi`](../../openapi) package generates an OpenAPI 3.0 document from Melody's registered routes and DTO types, with no external dependencies. It powers a CLI command that emits a spec for generating typed clients.

## Scope

OpenAPI generation is opt-in. The generator reads route metadata from the [`http`](HTTP.md) router (paths, methods, path parameters) and merges it with a userland-provided [`Registry`](../../openapi/registry.go) that describes request/response DTOs and summaries per route name. Schemas are derived by reflection over Go structs, reusing the same `json` and `validate` struct tags the rest of the framework uses.

## Responsibilities

- Describe operations per route:
    - [`Registry`](../../openapi/registry.go)
    - [`NewRegistry`](../../openapi/registry.go)
    - [`Descriptor`](../../openapi/registry.go)
    - [`TypeOf`](../../openapi/registry.go)
- Generate the document:
    - [`Generate`](../../openapi/generator.go)
    - [`Document`](../../openapi/document.go) and the OpenAPI 3.0 object model
- Build schemas from Go types:
    - [`schemaFromType`](../../openapi/schema.go) (internal; reads `json` and `validate` tags)
- Emit the document from the CLI:
    - [`GenerateCommand`](../../openapi/generate_command.go)
    - [`NewGenerateCommand`](../../openapi/generate_command.go)

## How generation works

[`Generate`](../../openapi/generator.go) walks the router's `RouteDefinition` list. For each route it:

- converts the Melody pattern to an OpenAPI path — `:id` and `{id}` segments become `{id}` path parameters;
- maps each HTTP method to an `Operation` keyed by the route name as `operationId`;
- enriches the operation from the [`Registry`](../../openapi/registry.go) when a [`Descriptor`](../../openapi/registry.go) is registered for the route name (summary, tags, request body, responses).

Schemas come from reflection over the DTO types in the descriptor. Struct fields use their `json` tag for the property name (skipping `-` and unexported fields), and `validate` tags shape the schema:

- `notBlank` / `notEmpty` → the property is added to `required`;
- `email` → `format: email`;
- `min` / `max` → `minLength` / `maxLength`;
- `greaterThan` → `minimum`;
- `regex` → `pattern`.

`time.Time` maps to `string` / `date-time`; slices to arrays; maps to objects with `additionalProperties`.

## Usage

Describe operations in a registry and register the command:

```go
registry := openapi.NewRegistry()
registry.Describe("products.create", openapi.Descriptor{
	Summary:     "Create a product",
	Tags:        []string{"products"},
	RequestType: openapi.TypeOf[ProductCreateRequest](),
	Responses: map[int]reflect.Type{
		201: openapi.TypeOf[ProductView](),
	},
})

command := openapi.NewGenerateCommand(
	openapi.Info{Title: "Example API", Version: "1.0.0"},
	registry,
)
```

For the common single-request / single-response route, [`DescribeTyped[Req, Resp]`](../../openapi/describe_typed.go) takes the types as parameters instead of a `Descriptor` literal:

```go
openapi.DescribeTyped[ProductCreateRequest, ProductView](
	registry, "products.create", 201,
	openapi.WithSummary("Create a product"),
	openapi.WithTags("products"),
)
```

Use `Describe` directly for no-body or multi-response routes (add extra responses with `WithResponse[T](status)`).

Run it to emit the document:

```sh
app melody:openapi:generate            # prints to stdout
app melody:openapi:generate --out openapi.json
```

The example application registers a registry (`config/openapi.go`) and the command, describing the product-create and i18n-greeting routes.

## Footguns & caveats

- Generation is opt-in and userland-wired; routes without a registered descriptor still appear (path, method, path parameters) but with a single `default` response and no body.
- The router normalizes trailing slashes, so generated path keys have no trailing slash even when the route pattern does.
- `validate` tag parsing splits on commas; a `regex` pattern containing a comma is not supported by the schema mapping.
- Schemas are inlined (no `$ref`/`components` reuse). Recursive types are broken with a generic `object` schema to avoid infinite recursion.

## Userland API

### Types (`openapi`)

- [`Document`, `Info`, `Components`, `PathItem`, `Operation`, `Parameter`, `RequestBody`, `ResponseObject`, `MediaType`, `Schema`](../../openapi/document.go)
- [`Registry`](../../openapi/registry.go)
- [`Descriptor`](../../openapi/registry.go)
- [`GenerateCommand`](../../openapi/generate_command.go)

### Functions (`openapi`)

- [`NewRegistry() *Registry`](../../openapi/registry.go)
- [`(*Registry).Describe(routeName string, descriptor Descriptor) *Registry`](../../openapi/registry.go)
- [`TypeOf[T any]() reflect.Type`](../../openapi/registry.go)
- [`Generate(info Info, routeDefinitions []httpcontract.RouteDefinition, registry *Registry) *Document`](../../openapi/generator.go)
- [`NewGenerateCommand(info Info, registry *Registry) *GenerateCommand`](../../openapi/generate_command.go)
