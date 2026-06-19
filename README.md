# Star

<p align="center">
  <strong>A lightweight HTTP framework for Go, built on <code>net/http</code>.</strong>
</p>

<p align="center">
  <a href="https://github.com/ctxiaoshi/star/blob/main/LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License"></a>
  <a href="https://github.com/ctxiaoshi/star"><img src="https://img.shields.io/badge/version-1.0.0--beta-blue" alt="Version"></a>
  <a href="https://pkg.go.dev/github.com/ctxiaoshi/star"><img src="https://img.shields.io/badge/go.dev-reference-007d9c" alt="Go Reference"></a>
</p>

Star is a lightweight Go web framework built on top of the standard `net/http` package. It wraps routing, parameter binding, middleware, route guards, unified responses, WebSocket, static files, and SPA fallback into a small API surface. It is suitable for quickly building HTTP services, backend APIs, and small full-stack applications.

## Installation

Requires Go 1.26+.

```bash
go get github.com/ctxiaoshi/star@v1.0.0-beta
```

## Quick Start

```go
package main

import "github.com/ctxiaoshi/star"

func main() {
	star.New(":8080")

	star.Use(star.Route{
		Method: star.GET,
		Path:   "/hello",
		Handler: func(ctx *star.Context) *star.Response {
			return &star.Response{
				Status:  true,
				Message: "success",
				Data:    "Hello, Star!",
			}
		},
	})

	star.Run()
}
```

```bash
$ curl http://localhost:8080/hello
```

```json
{
    "status": true,
    "message": "success",
    "data": "Hello, Star!"
}
```

## Application Model

Star follows a simple three-step model:

1. `star.New(":8080")` — create the application instance
2. `star.Use(...)` — register routes, middleware, or guards
3. `star.Run()` — flatten routes, apply middleware, and start the HTTP server

`New()` registers three default middleware:

- `RecoverMiddleware()` — catches panics and returns 500
- `RequestIdMiddleware()` — generates a UUID request ID, writes it to `Request-ID` response header
- `LogMiddleware()` — logs method, status code, path, IP, request ID, duration, and user agent

To disable default middleware:

```go
star.New(":8080", false)
```

## Routing

### Basic Routes

```go
star.Use(star.Route{
	Method:  star.GET,
	Path:    "/users",
	Handler: func(ctx *star.Context) *star.Response {
		return &star.Response{Status: true, Message: "success"}
	},
})
```

Supported methods: `star.GET`, `star.POST`, `star.PUT`, `star.DELETE`, `star.PATCH`, `star.OPTIONS`, `star.HEAD`, `star.ANY`.

### Path Parameters

Path parameters are defined with `{name}`. Default type is `str`.

```go
star.Use(star.Route{
	Method: star.GET,
	Path:   "/user/{id:int}",
	Handler: func(ctx *star.Context) *star.Response {
		id := ctx.GetParam("id") // int64
		return &star.Response{Status: true, Data: id}
	},
})
```

| Type | Go Value | Examples |
|------|----------|----------|
| `str` | `string` | `hello` |
| `int` | `int64` | `42` |
| `float` | `float64` | `3.14` |
| `bool` | `bool` | `true`, `false`, `1`, `0` |
| `time` | `time.Time` | `2024-01-01`, `2024-01-01T00:00:00Z`, `1704067200` |

### Optional Parameters

Optional parameters use `?` with a default value:

```go
star.Use(star.Route{
	Method: star.GET,
	Path:   "/list/{page?:int:1}/{size?:int:10}",
	Handler: func(ctx *star.Context) *star.Response {
		return &star.Response{Status: true, Data: ctx.GetParams()}
	},
})
```

`GET /list` returns `{"page":1,"size":10}`. Optional parameters must come after required ones.

### Nested Routes

Group routes with `Children`. Child routes inherit the parent path, middleware, `Meta`, and `Method`.

```go
star.Use(star.Route{
	Path: "/api",
	Children: []star.Route{
		{
			Method:  star.GET,
			Path:    "v1/status",
			Handler: func(ctx *star.Context) *star.Response {
				return &star.Response{Status: true, Data: "v1 running"}
			},
		},
	},
})
```

Registers `GET /api/v1/status`.

### Route Metadata

`Meta` attaches business metadata to routes. Parent and child `Meta` maps are shallow-merged; child values override parent values.

```go
star.Use(star.Route{
	Path: "/admin",
	Meta: map[string]any{"auth": map[string]any{"role": "admin"}},
	Children: []star.Route{
		{
			Method: star.GET,
			Path:   "dashboard",
			Handler: func(ctx *star.Context) *star.Response {
				role := ctx.GetMeta("auth.role") // supports dot-path access
				return &star.Response{Status: true, Data: role}
			},
		},
	},
})
```

## DTO Binding & Validation

Star can automatically bind request data to structs. If the struct implements `Validator`, `Validate()` is called automatically.

```go
type Validator interface {
	Validate(ctx *star.Context) error
}
```

### Query Binding

```go
type UserQuery struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func (q UserQuery) Validate(ctx *star.Context) error {
	if q.Name == "" {
		return fmt.Errorf("name is required")
	}
	return nil
}

star.Use(star.Route{
	Method: star.GET,
	Path:   "/user/{id:int}",
	Query:  UserQuery{},
	Handler: func(ctx *star.Context) *star.Response {
		id := ctx.GetParam("id")
		query := ctx.GetQueryModel().(UserQuery)
		return &star.Response{Status: true, Data: map[string]any{"id": id, "query": query}}
	},
})
```

Manual query access:

```go
ctx.GetQuery("name")                            // string
ctx.GetQuery("age", star.StarTypeInt)           // int64
ctx.GetQueries(map[string]star.StarType{"age": star.StarTypeInt})
```

### Body Binding

Supports `application/json`, `application/x-www-form-urlencoded`, and `multipart/form-data`.

```go
type CreateUserBody struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func (b CreateUserBody) Validate(ctx *star.Context) error {
	if b.Name == "" || b.Email == "" {
		return fmt.Errorf("name and email are required")
	}
	return nil
}

star.Use(star.Route{
	Method: star.POST,
	Path:   "/user",
	Body:   CreateUserBody{},
	Handler: func(ctx *star.Context) *star.Response {
		body := ctx.GetBodyModel().(CreateUserBody)
		return &star.Response{Status: true, Message: "created", Data: body}
	},
})
```

Manual body access:

```go
ctx.GetBody("name")   // single field
ctx.GetBodyAll()      // entire body as map
```

## Responses

Handlers return `*star.Response`. The framework writes it as a unified JSON response with HTTP 200.

```go
type Response struct {
	Status  bool   `json:"status"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
	Extra   any    `json:"extra,omitempty"`
	Stack   string `json:"stack,omitempty"`
}
```

For manual control, use context methods:

```go
ctx.Json(201, map[string]any{"id": 1})
ctx.Html(200, "<h1>{{.Title}}</h1>", map[string]any{"Title": "Star"})
ctx.ResponseError(400, fmt.Errorf("bad request"))
```

When a handler returns `nil`, or the request has already been consumed by `ctx.Json()` / `ctx.Html()` / `ctx.ResponseError()`, the framework does not write a second response.

## Middleware

```go
type Middleware func(next star.Handler) star.Handler
```

### Global Middleware

```go
star.Use(func(next star.Handler) star.Handler {
	return func(ctx *star.Context) *star.Response {
		ctx.AddContext("start", time.Now())
		return next(ctx)
	}
})
```

### Route-Level Middleware

```go
star.Use(star.Route{
	Path: "/admin",
	Middleware: []star.Middleware{
		func(next star.Handler) star.Handler {
			return func(ctx *star.Context) *star.Response {
				ctx.AddContext("role", "admin")
				return next(ctx)
			}
		},
	},
	Handler: func(ctx *star.Context) *star.Response {
		return &star.Response{Status: true, Data: ctx.GetContext("role")}
	},
})
```

### Built-in Middleware

| Middleware | Description |
|-----------|-------------|
| `RecoverMiddleware()` | Recovers from panics, returns 500 |
| `RequestIdMiddleware()` | Generates a UUID request ID |
| `LogMiddleware()` | Writes HTTP access logs |
| `CorsMiddleware(config ...CorsConfig)` | Sets CORS headers, handles preflight |
| `RateLimitMiddleware(requests, per)` | IP + Path rate limiting, returns 429 when exceeded |

## Route Guards

### Before Guards

Run after route matching and before DTO binding. Return a non-nil `*Response` to interrupt the flow.

```go
star.Use(star.BeforeRouteGuard(func(ctx *star.Context) *star.Response {
	if ctx.GetRequestHeader("Authorization") == "" {
		return &star.Response{Status: false, Message: "unauthorized"}
	}
	return nil
}))
```

### After Guards

Run after the handler. Can replace the final response.

```go
star.Use(star.AfterRouteGuard(func(ctx *star.Context, resp *star.Response) *star.Response {
	if resp == nil {
		return nil
	}
	resp.Extra = map[string]any{"requestId": ctx.RequestID}
	return resp
}))
```

Guards execute in registration order.

## CORS

```go
star.ConfigureCors(star.CorsConfig{
	AllowOrigins:     []string{"https://example.com"},
	AllowMethods:     []string{"GET", "POST", "PUT", "DELETE"},
	AllowHeaders:     []string{"Content-Type", "Authorization"},
	ExposeHeaders:    []string{"Request-ID"},
	AllowCredentials: true,
	MaxAge:           86400,
})
```

Without configuration, `CorsMiddleware()` allows all origins, methods, and headers. `OPTIONS` requests return `204 No Content`.

## WebSocket

```go
star.Use(star.Route{
	Path:        "/ws",
	IsWebSocket: true,
	Handler: func(ctx *star.Context) *star.Response {
		conn := ctx.GetWebSocket()
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				break
			}
			conn.WriteMessage(1, msg)
		}
		return nil
	},
})
```

- `IsWebSocket: true` forces the method to `GET`
- Non-WebSocket handshake requests return `400`
- Set a custom `*websocket.Upgrader` via `WebsocketUpgrade`

## Static Files & SPA

### Static Files

```go
star.Use(star.Route{
	Path:      "/static",
	IsStatic:  true,
	StaticDir: "./static",
})
```

`GET /static/style.css` serves `./static/style.css`.

### SPA Fallback

```go
star.Use(star.Route{
	Path:     "/app",
	IsSPA:    true,
	SPADir:   "./web",
	SPAIndex: "index.html",
})
```

If a requested path does not map to a file, Star falls back to `index.html` so the frontend router can take over.

## Logging & Debugging

Enable debug mode:

```go
star.EnableDebug()
```

Debug mode enables `DEBUG`-level logs, and 500 error responses include the stack trace.

Enable log persistence:

```go
star.EnableLogSave()
```

Custom log configuration:

```go
star.EnableLogSave(star.Logger{
	FileDir: "./log",
	MaxSize: 30 * 1024, // KB; -1 disables size-based sharding
	MaxAge:  7,         // days; -1 disables cleanup
})
```

Log files are named by date: `2026-05-22.log`, `2026-05-22_001.log`, etc.

Disable terminal colors:

```bash
NO_COLOR=1 TERM=dumb
```

## Context Reference

### Request Info

| Method | Description |
|--------|-------------|
| `GetPath()` | Normalized request path |
| `GetRequestMethod()` | Raw HTTP method |
| `GetMethod()` | Matched route method |
| `GetClientIP()` | Client IP chain (parses `X-Forwarded-For`) |
| `GetOriginalRequest()` | Original `*http.Request` |
| `GetOriginalResponseWriter()` | Original `http.ResponseWriter` |

### Parameters & Models

| Method | Description |
|--------|-------------|
| `GetParam(key)` | Single path parameter |
| `GetParams()` | All path parameters |
| `GetQuery(key, typ ...StarType)` | Query value (optional type conversion) |
| `GetQueries(typMap ...map[string]StarType)` | All query values |
| `GetQueryModel()` | Bound Query DTO |
| `GetBody(key)` | Single body field |
| `GetBodyAll()` | Parsed body as map |
| `GetBodyModel()` | Bound Body DTO |

### Request Rewriting

| Method | Description |
|--------|-------------|
| `SetQueries(map[string]string)` | Rewrite query parameters |
| `SetBody(string)` | Rewrite request body |

### Response Control

| Method | Description |
|--------|-------------|
| `SetStatusCode(code)` | Set HTTP status code (once) |
| `GetStatusCode()` | Get current status code (default 200) |
| `SetHeader(key, value)` | Set response header |
| `AddHeader(key, value)` | Append response header |
| `RemoveHeader(key)` | Remove response header |
| `GetResponseHeader(key)` | Get response header |
| `GetResponseHeaders()` | Get all response headers |
| `Json(status, data)` | Write JSON response |
| `Html(status, tmpl, data...)` | Write HTML with LRU template caching |
| `ResponseSuccess(response)` | Write 200 JSON response |
| `ResponseError(code, err...)` | Write error response |

### Cookies

| Method | Description |
|--------|-------------|
| `GetCookie(key)` | Read a cookie |
| `GetCookies()` | Read all cookies |
| `SetCookie(key, value, maxAge)` | Set a cookie |
| `DeleteCookie(key)` | Delete a cookie |

### Context Data & Extensions

| Method | Description |
|--------|-------------|
| `AddContext(key, value)` | Write request-scoped data |
| `GetContext(key)` | Read context data (supports `a.b`, `items[0].id`) |
| `DelContext(key)` | Delete context data |
| `GetMeta(key)` | Read route metadata (supports nested paths) |
| `IsWebSocket()` | Check if request is a WebSocket handshake |
| `GetWebSocket()` | Get the WebSocket connection |

## API Reference

### Global Functions

| Function | Description |
|----------|-------------|
| `New(bind, useDefaultMiddleware...)` | Create the application instance |
| `Use(component)` | Register routes, middleware, or guards |
| `Run()` | Start the HTTP server |
| `EnableDebug()` | Enable debug mode |
| `EnableLogSave(config...)` | Enable log persistence |
| `ConfigureCors(config)` | Register CORS middleware |

`Use()` accepts: `star.Route`, `[]star.Route`, `star.Middleware`, `[]star.Middleware`, `star.BeforeRouteGuard`, `[]star.BeforeRouteGuard`, `star.AfterRouteGuard`, `[]star.AfterRouteGuard`.

### Utilities

| Function | Description |
|----------|-------------|
| `Now()` | Current time |
| `NowFormat(format)` | Current time formatted |
| `FormatTime(t, format)` | Format a time value |
| `ParseDateTime(s)` | Parse `2006-01-02 15:04:05` |
| `ParseTimestamp(s)` | Parse second or millisecond timestamps |
| `VerifyTimestamp(s)` | Validate a timestamp string |
| `GenerateUUID()` | Generate a UUID v4 |
| `VerifyUUID(s)` | Validate a UUID |
| `GetValueByPath(obj, path)` | Read nested values from map/struct/slice |

## License

MIT