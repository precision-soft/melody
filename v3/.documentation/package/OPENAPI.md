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
    - [`DescribeTyped[Req, Resp]`](../../openapi/describe_typed.go) with [`WithSummary`, `WithDescription`, `WithTags`, `WithResponse[T]`](../../openapi/describe_typed.go)
- Generate the document:
    - [`Generate`](../../openapi/generator.go)
    - [`Document`](../../openapi/document.go) and the OpenAPI 3.0 object model (`Document`/`Schema` carry `servers`/`security`/`tags`/`externalDocs` and `description`/`maximum`/`exclusiveMaximum`)
- Build schemas from Go types:
    - [`schemaFromType`](../../openapi/schema.go) (internal; reads `json` and `validate` tags)
- Emit the document from the CLI:
    - [`GenerateCommand`](../../openapi/generate_command.go)
    - [`NewGenerateCommand`](../../openapi/generate_command.go)
- Serve the document from a live route (e.g. `GET /openapi.json`):
    - [`SpecHandler`](../../openapi/spec_handler.go) (reuses `Generate` against the running router, so every load-balanced instance serves the same spec)
- Resolve the registry as a container service:
    - [`ServiceOpenApiRegistry`, `RegistryMustFromContainer`, `RegistryMustFromResolver`](../../openapi/service_resolver.go)

## How generation works

[`Generate`](../../openapi/generator.go) walks the router's `RouteDefinition` list. For each route it:

- converts the Melody pattern to an OpenAPI path — `:id` and `{id}` segments become `{id}` path parameters; a catch-all (`*rest...`, or a trailing `*rest`) becomes `{rest}` and **ends the documented path**, because the router's registration discards every segment written after a catch-all and never matches it — a mid-pattern `*name` without the dots is a single-segment wildcard and keeps its tail;
- maps each HTTP method to an `Operation` keyed by the route name as `operationId`. A route registered with **no** method list answers every verb, so it is documented on all eight path item verbs, each with its own operationId; a verb the format cannot model (the router registers any string) is named in the path item's `description` instead of being dropped without a trace;
- enriches the operation from the [`Registry`](../../openapi/registry.go) when a [`Descriptor`](../../openapi/registry.go) is registered for the route name (summary, tags, request body, responses). The responses are visited in status order, so component naming — and with it the whole document — is byte-stable across runs;
- never overwrites an operation another route already wrote: where two patterns converge on one converted path, the earlier registration wins, exactly as it does in the router's match order.

### A trailing optional parameter becomes two path items

The router serves a trailing optional parameter both ways, so one path key would describe only half of what answers. OpenAPI 3.0 forbids `required: false` on `in: path`, which leaves no way to say "optional" inside a single path item — so [`expandOptionalTailSegment`](../../openapi/generator.go) emits **both** shapes as separate path items:

| Melody pattern | Emitted paths              |
|----------------|----------------------------|
| `/users/:id?`  | `/users` and `/users/{id}` |
| `/users/{id?}` | `/users` and `/users/{id}` |

Only the final segment may carry the `?` marker, so the shortened form is always the pattern minus its last segment (a pattern that shortens to nothing becomes `/`).

The two operations share the descriptor's summary, description, tags, request body and responses. They differ in two ways:

- the shortened form declares **no path parameter**, since the segment it would describe is not there;
- it gets a distinct operation id, `<id>.without.<parameter>` — `users.show.without.id` beside `users.show` — because operation ids must be unique across the document.

A route **explicitly registered** at the shortened path describes it better than this mirror does, so its own operation always wins, regardless of the order the two routes were registered in: the mirror is skipped when an operation for that method already exists, and an explicit route overwrites a mirror that was written first.

Schemas come from reflection over the DTO types in the descriptor. Struct fields use their `json` tag for the property name (skipping `-` and unexported fields), and `validate` tags shape the schema. The mapping mirrors what the runtime validator actually enforces, and the rule that governs every branch is **fail-closed**: the document may refuse more than the server (each over-approximation is declared in [`schema.go`](../../openapi/schema.go) with its reason), but it never advertises a value the validator refuses:

- `notBlank` → the property is `required` and non-nullable on every kind; on a genuine string it adds a `minLength: 1` floor, and on **any other shape** — `[]byte` and `time.Time` included, both rendered as strings — the constraint refuses the value itself (`"value must be a string"`), so the field is advertised unsatisfiable;
- `notEmpty` → `required`, non-nullable, and the matching floor per shape (`minLength` / `minItems` / `minProperties`); a struct or scalar carrying it is unsatisfiable, exactly as the validator rejects those outright;
- `email`, `alpha`, `numeric`, `alphanumeric` → `format: email` / an anchored character-class `pattern` on a genuine string; on any other shape the constraint refuses every non-null value while passing a nil pointer, so a nullable field is advertised as accepting exactly `null` and a non-nullable one as unsatisfiable;
- `min` / `max` → `minLength` / `maxLength` on genuine strings only, mirroring the validator's string-length checks; on every other shape the same nil-skipping refusal applies. `min`/`max` are deliberately never emitted as numeric `minimum`/`maximum` or array `minItems`/`maxItems`, because the validator enforces no such bound;
- `greaterThan` / `lessThan` → exclusive `minimum` / `maximum` on numeric fields (a negative bound is a legitimate declaration); on a non-numeric field the constraint rejects every value, so the field is unsatisfiable;
- `regex` → `pattern`, emitted verbatim on a genuine string when it compiles; an uncompilable pattern advertises `maxLength: 0` (the validator accepts only the empty string there), and on a non-string shape the non-string refusal wins;
- a rule the validator refuses **at construction** — a bare parameterized constraint (`min`, `max`, `greaterThan`, `lessThan`, `regex` with no value), an empty `regex` pattern, a malformed or negative `min`/`max` bound, a malformed tag, a tag that parses to no rule at all — fails the whole field closed before any value is examined: the field is advertised unsatisfiable **and listed `required`**, because the absent zero value goes through the same refused rule and a payload omitting the field is rejected too.

Slices map to arrays (a `[]byte` to `string` / `format: byte`); maps to objects with `additionalProperties`. A named struct type is emitted once into `components/schemas` and referenced by `$ref`, so a type reused across operations is defined a single time. A nullable pointer to a named struct is wrapped as `{"allOf":[{"$ref":…}],"nullable":true}` (OpenAPI 3.0 ignores `$ref` siblings, so the nullability would otherwise be lost). Self-referential types terminate through the `$ref`. A tag on a `$ref` field is read against the shape the component stands for — a struct or a named collection — with the same refusals as inline; an unsatisfiable reference is contradicted with a type-agnostic `not: {}` beside the `$ref`.

### The validator lockstep

[`schema.go`](../../openapi/schema.go) is a hand-written mirror of the [`validation`](VALIDATION.md) package's semantics: the production code deliberately does **not** import `validation`, so the generator keeps a dependency-free surface. What holds the two in step is not the prose — it is [`schema_test.go`](../../openapi/schema_test.go), the only file in the tree where `openapi` reaches `validation` (a test-only import). Its oracle is semantic, over values: every tag class crossed with every field shape is built into a real struct type, the document's verdict on a value is read from the generated facets alone, the validator's verdict comes from `validation.NewValidator().Validate` on the same value, and the document is never allowed to advertise a value — or an absence — the validator refuses. The declared divergences (today exactly one: `minLength` cannot express `notBlank`'s whitespace-only rejection) are enumerated in the test with their reasons.

When the validator's semantics change, this mirror must change in the same session — and the lockstep suite is what turns a forgotten half into a red test instead of a silently wrong published contract. Do not replace it with assertions on the mirror's own predicates: an oracle written on predicates pins the branches of the current repair and goes blind at the next divergence.

### Types that marshal themselves

`encoding/json` promotes an embedded type's `MarshalJSON` to the enclosing type and then emits that embed's encoding for the whole value, never visiting the sibling fields — so advertising the promoted properties of such a struct would describe a body that is never sent.

The generator follows that rule as far as reflection can take it: it advertises the promoted encoding where the wire form is derivable from the type, and keeps the object schema everywhere else. In practice the derivable case is an embedded `time.Time` — `time.Time` mapping to `string` / `date-time` is an instance of the general rule rather than a special case — because for any other codec the bytes are whatever its `MarshalJSON` writes, which reflection cannot read.

Which embed's codec is promoted is resolved the way Go builds a method set, not by reachability ([`promotedMarshalerOrigin`](../../openapi/schema.go)): the codec at the **shallowest embedding depth** wins, embeds tied at that depth promote nothing, and a **non-struct** embed competes like any other. A depth-1 marshaler embed that writes an object therefore keeps its object schema even when a `time.Time` sits below it, and the `date-time` string is emitted **only** when `time.Time` is the type the promotion actually travelled from ([`promotesEmbeddedTimeCodec`](../../openapi/schema.go)).

The surrounding fields are described as an object in each of these cases:

- an enclosing type that does not itself implement `json.Marshaler` promotes nothing, so its own fields are described. Two embedded codecs tied at the shallowest depth are an instance of this rather than a separate rule — Go promotes neither, so the enclosing type is not a `json.Marshaler` at all. (Where one of the tied embeds is a `time.Time`, its `MarshalText` is then promoted unopposed and `encoding/json` falls back to it, so the wire is a date-time string while the schema stays an object; recognising that would mean modelling the whole codec precedence, not just the promoted-time case.)
- a promoted codec with a **pointer receiver** reached through a pointer embed, and a codec reached through an **embed cycle**. Reflection cannot attribute either to a declaring type, so the promotion is left unresolved and the object schema stands — the safe direction, since guessing the other way would advertise a string for every such shape.
- a promoted `MarshalText` fallback, a user `json.Marshaler` embed, and an **embedded marshaler interface**. For those the bytes are whatever the codec writes, which reflection cannot read, so the fields are described rather than a wire form guessed at. A pointer-receiver codec on a **value** embed also lands here, and correctly: it stays out of the enclosing value's method set, so `encoding/json` spreads the fields for a value exactly as the schema says.

One shape is out of reach in the opposite direction: a `MarshalJSON` **declared on the enclosing type** while an embed also carries one. `reflect.Method` carries no declaring type, so the declaration and the promotion are indistinguishable and such a type resolves to its embed — a `time.Time` embed then keeps the `date-time` string while the wire form is whatever the declared codec writes.

The framework validator resolves the promotion with the same rules and **skips** a struct whose promoted codec is `time.Time`'s ([`promotesValidationTimeCodec`](../../validation/validator.go)), so the mirror and the validator agree on this shape: the mirror advertises the date-time string and the validator enforces none of the constraints the struct declares. They previously disagreed — the mirror advertised the string while the validator enforced, say, `notBlank` on a sibling `encoding/json` can never populate, which rejected every body the type is able to decode.

### Component naming

A component key is derived from the Go type name, sanitized to the key grammar ([`sanitizeComponentName`](../../openapi/schema.go)). Two distinct types that share a bare name (for example `product.Request` and `order.Request`) are disambiguated with a numeric suffix — `Request`, then `Request2`, `Request3`.

**Instantiated generics** need more than that. Go reports type arguments as part of the type name — `Page[github.com/acme/app/api.User]` — and neither the brackets nor the import paths survive as a component key: the key grammar admits only letters, digits, `.`, `-` and `_`, and a raw `/` splits the `$ref` JSON pointer into tokens that resolve to nothing. So import paths are dropped and the remaining punctuation folded to a single underscore:

| Go type name                         | Component key   | `$ref`                               |
|--------------------------------------|-----------------|--------------------------------------|
| `Page[github.com/acme/app/api.User]` | `Page_api.User` | `#/components/schemas/Page_api.User` |

Dropping the import path keeps the qualified type name that carries the meaning, and the key stays derived only from the type, so it is stable across runs. Two instantiations that differ only in the folded punctuation fold to the same key; the numeric suffix above then separates them (`Page_api.User2`).

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

A relative `--out` is anchored at the project directory (`kernel.project_dir`), exactly as the wiring command anchors its own, with the parent directories created on the way; the write lands through a temp file and a rename, and an existing file that does not hold a JSON document — someone's source a mistyped `--out` points at — is refused rather than replaced. The command is auto-registered by the application when the container carries `ServiceOpenApiRegistry`; [`NewGenerateCommandFromContainer`](../../openapi/generate_command.go) is the constructor that resolves the `Info` and the `Registry` at run time, and it says on its writer when no `ServiceOpenApiInfo` is registered, since the tolerant [`InfoFromResolver`](../../openapi/service_resolver.go) then answers an empty `Info` and the document's required title and version are empty strings.

The example application registers a registry (`config/openapi.go`) and the command, describing the product-create and i18n-greeting routes.

## Footguns & caveats

- Generation is opt-in and userland-wired; routes without a registered descriptor still appear (path, method, path parameters) but with a single `default` response and no body.
- **The document enumerates every route the router carries** — internal, administrative and authentication routes included, with their methods and path-parameter names. There is no per-route opt-out. An application that mounts [`SpecHandler`](../../openapi/spec_handler.go) decides who can read that enumeration with the same firewall rules as any other route; mounting it public, as the example does, publishes the whole route table deliberately.
- [`Registry.Describe`](../../openapi/registry.go) belongs to **boot** — module construction, before the application serves. It writes a plain map the spec handler reads on the request path with nothing synchronizing the two, so a `Describe` issued while requests are in flight is a concurrent map write, which Go answers by killing the process.
- [`SpecHandler`](../../openapi/spec_handler.go) regenerates the whole document on every request — the full reflection walk included. The document only changes at boot, so a deployment that expects the route to be hammered should cache the response in front of it (a reverse-proxy cache, or a handler of its own that generates once).
- A `regex` pattern is validated with Go's RE2 and emitted verbatim, while OpenAPI 3.0 prescribes the ECMA-262 dialect for `pattern`; keep to the common subset (no `(?i)` inline flags, no `\p{...}` classes) or the produced document fails downstream validators.
- The router normalizes trailing slashes, so generated path keys have no trailing slash even when the route pattern does.
- `validate` tag rules are comma-separated, and the schema mapping splits them exactly as the runtime validator does: a comma inside a `()` group, a `[...]` character class, a `{n,m}` quantifier, a quoted parameter value or behind a backslash stays part of its pattern. Only a bare top-level comma is a rule separator — in both layers alike, since that is the tag grammar itself — so a pattern that genuinely needs one escapes it (`\,`).
- Reused named struct types are emitted once into `components/schemas` and referenced by `$ref` (see "How generation works"); an unnamed (anonymous) cyclic struct falls back to a generic `object` to avoid infinite recursion.
- **A misconfigured tag is published as an unsatisfiable required field**, not hidden: a bare `min`, a `max=-1`, an empty `regex` pattern or a malformed bound makes the validator reject every payload, and the document says so (an impossible facet window, `not: {}` on a reference) instead of advertising a satisfiable contract the server refuses on every request. A spec-driven client failing on such a field is reporting the server's real behaviour — fix the tag.
- A `validate` tag declared **inside** a struct whose promoted json codec is `time.Time`'s is neither advertised nor enforced: the schema for such a value is a `date-time` string with no properties to hang a constraint on, and the validator skips the struct outright. A tag on the **field holding** that struct still applies, since it constrains the field rather than anything inside it.

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
- [`DescribeTyped[Req, Resp any](registry *Registry, routeName string, status int, options ...DescribeOption)`](../../openapi/describe_typed.go) with `WithSummary`, `WithDescription`, `WithTags`, `WithResponse[T any](status int)`
- [`Generate(info Info, routeDefinitions []httpcontract.RouteDefinition, registry *Registry) *Document`](../../openapi/generator.go)
- [`NewGenerateCommand(info Info, registry *Registry) *GenerateCommand`](../../openapi/generate_command.go)
- [`NewGenerateCommandFromContainer() *GenerateCommand`](../../openapi/generate_command.go) — the auto-registered form, resolving the `Info` and the `Registry` from the container at run time
- [`SpecHandler(info Info, registry *Registry) httpcontract.Handler`](../../openapi/spec_handler.go)
- [`RegistryMustFromContainer(...)`, `RegistryMustFromResolver(...)`](../../openapi/service_resolver.go) — resolve the registry registered under `ServiceOpenApiRegistry`
- [`InfoFromResolver(resolver containercontract.Resolver) Info`](../../openapi/service_resolver.go) — the tolerant reader of `ServiceOpenApiInfo`, answering an empty `Info` when none is registered
