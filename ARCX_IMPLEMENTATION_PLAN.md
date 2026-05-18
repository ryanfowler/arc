# arcx Implementation Plan

This plan is for an agent building `github.com/ryanfowler/arc/arcx`, a higher-level package on top of the existing `arc` router. The goal is to make common HTTP application tasks simple while preserving Arc's core values: standard `net/http` compatibility, predictable behavior, and low routing overhead.

Do not expand the core `arc` package unless a change is truly required. `arcx` should wrap `*arc.Router` and use its existing public API.

## Goals

- Provide ergonomic request handling with error-returning handlers.
- Make JSON body decoding and JSON response encoding concise.
- Bind path parameters, query parameters, headers, cookies, and request bodies into typed structs.
- Centralize HTTP error handling.
- Keep middleware compatible with ordinary `net/http` middleware.
- Avoid adding routing behavior; let `arc.Router` continue to own matching, method handling, subrouters, mounts, hosts, and path parameters.
- Cache reflection work so the convenience layer is practical for real services.

## Non-Goals

- Do not turn Arc into a full web framework.
- Do not add dependency injection, lifecycle management, background jobs, template rendering, sessions, or authentication primitives.
- Do not require users to abandon `http.Handler`, `http.HandlerFunc`, or standard middleware.
- Do not add OpenAPI generation in the first implementation. Keep the typed API compatible with future schema generation.
- Do not add third-party JSON or validation dependencies in the initial version.

## Target Package Layout

Create a child package:

```text
arcx/
  router.go
  context.go
  handler.go
  bind.go
  bind_plan.go
  convert.go
  codec.go
  error.go
  response.go
  options.go
  router_test.go
  bind_test.go
  codec_test.go
  error_test.go
  benchmark_test.go
```

Keep files small and organized by behavior. Tests should be in package `arcx`, not `arcx_test`, unless black-box coverage is specifically useful.

## Public API Shape

The basic usage should look like this:

```go
r := arcx.New()

r.Get("/users/{id}", arcx.JSON(func(c *arcx.Context, in GetUser) (User, error) {
	return users.Get(c.Context(), in.ID, in.Include)
}))

type GetUser struct {
	ID      int64    `param:"id"`
	Include []string `query:"include"`
}
```

And imperative handlers should remain good:

```go
r.Post("/users/{id}", func(c *arcx.Context) error {
	id, err := c.Param("id").Int64()
	if err != nil {
		return err
	}

	var body UpdateUser
	if err := c.Decode(&body); err != nil {
		return err
	}

	user, err := users.Update(c.Context(), id, body)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, user)
})
```

## Router

Implement:

```go
type Router struct {
	base *arc.Router
	cfg  *Config
}

func New(opts ...Option) *Router
func Wrap(r *arc.Router, opts ...Option) *Router
func (r *Router) Base() *arc.Router
func (r *Router) ServeHTTP(http.ResponseWriter, *http.Request)
```

Route registration:

```go
type Handler func(*Context) error

func (r *Router) Handle(method, pattern string, h Handler)
func (r *Router) TryHandle(method, pattern string, h Handler) error
func (r *Router) HandleAll(pattern string, h Handler)
func (r *Router) TryHandleAll(pattern string, h Handler) error

func (r *Router) Get(pattern string, h Handler)
func (r *Router) Post(pattern string, h Handler)
func (r *Router) Put(pattern string, h Handler)
func (r *Router) Patch(pattern string, h Handler)
func (r *Router) Delete(pattern string, h Handler)
func (r *Router) Head(pattern string, h Handler)
func (r *Router) Options(pattern string, h Handler)
```

Grouping and interoperability:

```go
func (r *Router) Use(mw ...arc.Middleware)
func (r *Router) SubRouter(pattern string) *Router
func (r *Router) TrySubRouter(pattern string) (*Router, error)
func (r *Router) Host(pattern string) *Router
func (r *Router) TryHost(pattern string) (*Router, error)
func (r *Router) Mount(pattern string, h http.Handler)
func (r *Router) TryMount(pattern string, h http.Handler) error

func (r *Router) SetNotFound(h Handler)
func (r *Router) SetMethodNotAllowed(h Handler)
func (r *Router) SetStrictSlash(strict bool)
func (r *Router) SetImplicitHead(enabled bool)
```

`Use` must accept `arc.Middleware`, which is the same shape as standard `func(http.Handler) http.Handler`. This preserves compatibility with existing middleware.

The router should adapt `Handler` to `http.HandlerFunc` by constructing a `Context`, invoking the handler, and passing returned errors to the configured error handler.

## Context

Implement:

```go
type Context struct {
	ResponseWriter http.ResponseWriter
	Request        *http.Request
	// unexported config pointer
}

func (c *Context) Context() context.Context
func (c *Context) Pattern() string
func (c *Context) Header() http.Header
func (c *Context) Status(status int) error
func (c *Context) NoContent(status int) error
func (c *Context) JSON(status int, v any) error
func (c *Context) Decode(v any) error

func (c *Context) Param(name string) Value
func (c *Context) Query(name string) Value
func (c *Context) HeaderValue(name string) Value
func (c *Context) Cookie(name string) Value
```

`Context` should not hide the underlying request or response writer. Users must be able to drop down to ordinary `net/http` immediately.

## Value Helpers

Add a lightweight `Value` type for imperative parsing:

```go
type Value struct {
	value string
	ok    bool
	name  string
	where string
}

func (v Value) String() string
func (v Value) Required() (string, error)
func (v Value) Bool() (bool, error)
func (v Value) Int() (int, error)
func (v Value) Int64() (int64, error)
func (v Value) Uint64() (uint64, error)
func (v Value) Float64() (float64, error)
func (v Value) Duration() (time.Duration, error)
func (v Value) Time(layout string) (time.Time, error)
```

Missing optional values should parse as zero values only where that behavior is explicit and documented. `Required` should return a binding-style error when the value is absent or empty.

## Typed Handler Adapters

Because Go methods cannot have their own type parameters, use generic adapter functions:

```go
func JSON[In, Out any](fn func(*Context, In) (Out, error)) Handler
func NoContent[In any](fn func(*Context, In) error) Handler
func Raw(fn func(*Context) error) Handler
func FromHTTP(h http.Handler) Handler
```

`JSON` should:

1. Build or fetch a cached binding plan for `In`.
2. Bind request data into `In`.
3. Validate `In` if it implements a validation interface.
4. Call the user function.
5. Encode the returned value using the configured codec.

`NoContent` should do the same input binding and validation, then send `204 No Content` unless the handler already wrote a response. Avoid complicated response-written detection in the MVP unless there is a clean implementation; it is acceptable for `NoContent` to always write 204 after nil error.

`Raw` is mostly an identity helper for readability.

`FromHTTP` should allow mounting ordinary handlers into `arcx` route registrations.

## Response Model

Support direct return values and explicit response metadata:

```go
type Response[T any] struct {
	Status int
	Header http.Header
	Body   T
}

func OK[T any](body T) Response[T]
func Created[T any](body T) Response[T]
func Accepted[T any](body T) Response[T]
func Status[T any](status int, body T) Response[T]
func NoBody(status int) Response[struct{}]
```

`JSON` must detect `Response[T]` and use its status/header/body. For plain `Out`, default to `200 OK`.

Be careful with generic detection. A practical approach is to define an unexported interface implemented by `Response[T]`, for example:

```go
type responseValue interface {
	writeResponse(*Context) error
}
```

Then `Response[T]` can satisfy it.

## Binding

Bind a single request struct from tags:

```go
type CreateUser struct {
	AccountID string         `param:"accountID"`
	DryRun    bool           `query:"dry_run,default=false"`
	Token     string         `header:"Authorization"`
	SessionID string         `cookie:"sid"`
	Body      CreateUserBody `body:"json"`
}
```

Supported tags in the first version:

```text
param:"name"
query:"name"
header:"name"
cookie:"name"
body:"json"
```

Supported tag options:

```text
required
default=value
```

Examples:

```go
Limit int    `query:"limit,default=25"`
Query string `query:"q,required"`
```

Parsing should support:

- `string`
- `bool`
- signed integers
- unsigned integers
- floats
- `time.Duration`
- `time.Time` with RFC3339 as the default
- slices from repeated query/header values
- pointers for optional values
- types implementing `encoding.TextUnmarshaler`

Reject unsupported field shapes with clear errors during plan construction when possible.

### Body Binding Rules

- At most one field may use `body:"json"` in the MVP.
- Body fields may be struct, pointer to struct, map, slice, or any JSON-decodable type.
- If no body tag exists, typed handlers should not read the request body.
- Empty body should be an error when a body field is required. Decide whether body fields are required by default; document it clearly. Recommended: body fields are required by default unless they are pointers and not tagged `required`.

### Validation

After binding, call:

```go
type Validator interface {
	Validate() error
}

type ContextValidator interface {
	Validate(context.Context) error
}
```

Call `ContextValidator` first if implemented, otherwise `Validator`.

Do not add validation tags beyond `required` in the first version.

## Binding Plan Cache

Avoid scanning struct tags on every request.

Implement a cache keyed by `reflect.Type`:

```go
var bindPlans sync.Map // map[reflect.Type]*bindPlan
```

A `bindPlan` should contain field index paths and converter functions. Build plans once per input type.

Plan construction should happen when `arcx.JSON(fn)` or `arcx.NoContent(fn)` is called, not on first request, so invalid handler input types fail during route setup.

Registration-time panics are acceptable for the convenience adapters, matching Arc's existing startup-oriented registration style. Also consider `TryJSON` later if needed, but do not build it in the MVP.

## Codecs

Start with JSON and make codec replacement possible.

```go
type Codec interface {
	Decode(*http.Request, any) error
	Encode(http.ResponseWriter, *http.Request, int, any) error
}

type JSONOptions struct {
	MaxBodyBytes          int64
	DisallowUnknownFields bool
}
```

Options:

```go
func WithJSONOptions(JSONOptions) Option
func WithCodec(contentType string, codec Codec) Option
func WithErrorHandler(ErrorHandler) Option
```

Default JSON behavior:

- Set `Content-Type: application/json; charset=utf-8` for JSON responses unless already set.
- Use `json.Decoder`.
- If `DisallowUnknownFields` is true, call `DisallowUnknownFields`.
- If `MaxBodyBytes > 0`, wrap the body with `http.MaxBytesReader`.
- Reject trailing non-whitespace JSON after the first value.

Do not perform content negotiation in the MVP. Use JSON when a handler asks for JSON. Later versions can support `Accept` and `Content-Type` negotiation.

## Errors

Define:

```go
type HTTPError struct {
	Status  int
	Code    string
	Message string
	Err     error
}

func (e *HTTPError) Error() string
func (e *HTTPError) Unwrap() error
```

Helpers:

```go
func BadRequest(err error) error
func Unauthorized(message string) error
func Forbidden(message string) error
func NotFound(message string) error
func Conflict(message string) error
func Unprocessable(err error) error
func Internal(err error) error
```

Error handler:

```go
type ErrorHandler func(*Context, error)
```

Default mapping:

- `*HTTPError`: use its status/code/message.
- binding and decode errors: `400 Bad Request`.
- validation errors: `422 Unprocessable Entity`.
- unknown errors: `500 Internal Server Error`.

Default JSON shape:

```json
{
  "error": {
    "code": "bad_request",
    "message": "invalid query parameter limit"
  }
}
```

Avoid leaking internal error details for unknown 500s. Use a generic message for unknown errors.

## Options and Config

Use immutable-ish config copied into child routers:

```go
type Config struct {
	codecs map[string]Codec
	json JSONOptions
	errorHandler ErrorHandler
}

type Option func(*Config)
```

When creating `SubRouter` or `Host`, copy the config pointer if config is immutable after `New`, or deep-copy the config if options can mutate later. Prefer immutable config for simplicity.

## Interop With arc

- `arcx.Router` must satisfy `http.Handler`.
- `arcx.Router.Base()` should return the underlying `*arc.Router`.
- `arcx.Wrap(base)` should let users add `arcx` routes to an existing router.
- Middleware must remain `arc.Middleware`.
- `Mount` should accept ordinary `http.Handler`.
- `SetNotFound` and `SetMethodNotAllowed` should accept `arcx.Handler`, but users can always call `Base().SetNotFound` if they need raw handlers.

Do not depend on unexported `arc` internals.

## Implementation Order

1. Create `arcx` package skeleton with `Router`, `Handler`, `Context`, options, and handler adaptation.
2. Implement route registration methods by delegating to the underlying `arc.Router`.
3. Implement `Context.JSON`, `Context.Decode`, and the default JSON codec.
4. Implement `HTTPError`, error helpers, and default error handler.
5. Implement `Value` helpers for `Param`, `Query`, `HeaderValue`, and `Cookie`.
6. Implement typed adapters `JSON`, `NoContent`, `Raw`, and `FromHTTP`.
7. Implement binding plan construction for structs.
8. Implement converters and bind execution for params/query/header/cookie.
9. Implement `body:"json"` binding.
10. Implement validation hooks.
11. Add benchmarks for raw `arc` route vs `arcx` imperative handler vs `arcx.JSON` typed handler.
12. Add examples to package docs and update `README.md` with a short `arcx` section only after the package is working.

## Required Tests

Router and context:

- `arcx.Router` serves routes through the underlying Arc router.
- `Get`, `Post`, and `Handle` delegate correctly.
- `TryHandle` returns Arc registration errors.
- `Use` applies ordinary middleware.
- `SubRouter` preserves Arc path parameter behavior.
- `Host` preserves Arc host parameter behavior.
- `Mount` accepts ordinary `http.Handler`.

JSON and responses:

- `Context.JSON` writes status, content type, and body.
- `Context.Decode` decodes valid JSON.
- Unknown fields are rejected when configured.
- Max body size is enforced when configured.
- `Response[T]` applies status and headers.
- Plain return values default to `200 OK`.

Errors:

- Returned `HTTPError` maps to the right status and JSON shape.
- Binding errors map to `400`.
- Validation errors map to `422`.
- Unknown errors map to `500` without leaking details.
- Custom error handler is called.

Binding:

- `param` binds from `req.PathValue`.
- `query` binds simple values and repeated slices.
- `header` binds values case-insensitively through `http.Header`.
- `cookie` binds cookies.
- `default` works for missing values.
- `required` errors for missing values.
- Numeric/bool/duration/time parse errors are clear.
- Pointer fields remain nil when optional and absent.
- `encoding.TextUnmarshaler` fields bind correctly.
- Multiple `body:"json"` fields are rejected at adapter construction.
- Unsupported input types fail at adapter construction.

Validation:

- `Validate() error` is called.
- `Validate(context.Context) error` is called preferentially when both are available.

Compatibility:

- `arcx.Router` can be passed to `httptest` and `http.Server`.
- `Base()` allows registering raw Arc routes next to `arcx` routes.

## Benchmarks

Add focused benchmarks, not broad framework comparisons:

```text
BenchmarkArcxImperativeStatic
BenchmarkArcxImperativeParam
BenchmarkArcxJSONQueryParam
BenchmarkArcxJSONPathQueryBody
BenchmarkArcxBindingPlanBuild
```

Compare only against Arc where useful. The intent is to catch regressions and quantify overhead, not to prove `arcx` is as fast as raw `arc`.

## Documentation Requirements

Add `arcx/doc.go` with:

- Package overview.
- Quick start.
- Typed handler example.
- Imperative handler example.
- Binding tags.
- Error handling summary.
- Note that `arcx` wraps `arc` and remains compatible with `net/http`.

Keep the root README focused on `arc`. Add only a small section pointing users to `arcx` once it exists.

## Design Constraints

- Use only the Go standard library plus the existing module dependencies unless there is a strong reason.
- Keep `arc` core untouched unless tests prove a small exported hook is necessary.
- Do not add global mutable behavior except the binding plan cache.
- Do not make request binding depend on route pattern parsing; use `req.PathValue`.
- Do not obscure `http.ResponseWriter` or `*http.Request`.
- Keep all new code formatted with `gofmt`.

## Acceptance Criteria

- `go test ./...` passes.
- The new `arcx` package has package documentation and examples.
- All public types and functions have useful doc comments.
- Typed JSON handlers support path/query/header/cookie/body binding.
- Returned errors consistently produce JSON error responses.
- Existing `arc` tests and benchmarks continue to pass without behavioral changes.
- Benchmarks exist for the main `arcx` paths.
