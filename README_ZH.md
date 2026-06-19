# Star

<p align="center">
  <strong>基于 <code>net/http</code> 的轻量级 Go Web 框架。</strong>
</p>

<p align="center">
  <a href="https://github.com/ctxiaoshi/star/blob/main/LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License"></a>
  <a href="https://github.com/ctxiaoshi/star"><img src="https://img.shields.io/badge/version-1.0.0--beta-blue" alt="Version"></a>
  <a href="https://pkg.go.dev/github.com/ctxiaoshi/star"><img src="https://img.shields.io/badge/go.dev-reference-007d9c" alt="Go Reference"></a>
</p>

Star 是一个基于 Go `net/http` 标准库的轻量级 Web 框架。它把路由、参数绑定、中间件、路由守卫、统一响应、WebSocket、静态文件和 SPA 回退封装在一个很小的 API 面里，适合用来快速写 HTTP 服务、后台接口或小型前后端一体应用。

## 安装

需要 Go 1.26+。

```bash
go get github.com/ctxiaoshi/star@v1.0.0-beta
```

## 快速开始

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

## 应用模型

Star 的入口只有三个核心动作：

1. `star.New(":8080")` — 创建应用实例
2. `star.Use(...)` — 注册路由、中间件或路由守卫
3. `star.Run()` — 展开路由、套用中间件并启动 HTTP Server

`New()` 默认注册三个内置中间件：

- `RecoverMiddleware()` — 捕获 panic 并返回 500
- `RequestIdMiddleware()` — 生成 UUID 请求 ID，写入 `Request-ID` 响应头
- `LogMiddleware()` — 记录方法、状态码、路径、IP、请求 ID、耗时和 UA

如需关闭默认中间件：

```go
star.New(":8080", false)
```

## 路由

### 基本路由

```go
star.Use(star.Route{
	Method:  star.GET,
	Path:    "/users",
	Handler: func(ctx *star.Context) *star.Response {
		return &star.Response{Status: true, Message: "success"}
	},
})
```

支持的方法：`star.GET`、`star.POST`、`star.PUT`、`star.DELETE`、`star.PATCH`、`star.OPTIONS`、`star.HEAD`、`star.ANY`。

### 路径参数

路径参数使用 `{name}` 定义，默认类型为 `str`。

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

| 类型 | Go 值 | 示例 |
|------|-------|------|
| `str` | `string` | `hello` |
| `int` | `int64` | `42` |
| `float` | `float64` | `3.14` |
| `bool` | `bool` | `true`、`false`、`1`、`0` |
| `time` | `time.Time` | `2024-01-01`、`2024-01-01T00:00:00Z`、`1704067200` |

### 可选参数

可选参数使用 `?`，必须提供默认值：

```go
star.Use(star.Route{
	Method: star.GET,
	Path:   "/list/{page?:int:1}/{size?:int:10}",
	Handler: func(ctx *star.Context) *star.Response {
		return &star.Response{Status: true, Data: ctx.GetParams()}
	},
})
```

`GET /list` 返回 `{"page":1,"size":10}`。可选参数必须放在必选参数之后。

### 嵌套路由

用 `Children` 组织分组路由。子路由会继承父级路径、局部中间件、`Meta` 和未显式声明的 `Method`。

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

注册为 `GET /api/v1/status`。

### 路由元数据

`Meta` 用来给路由挂载业务侧元信息。父子路由的 `Meta` 会浅合并，同名字段以子路由为准。

```go
star.Use(star.Route{
	Path: "/admin",
	Meta: map[string]any{"auth": map[string]any{"role": "admin"}},
	Children: []star.Route{
		{
			Method: star.GET,
			Path:   "dashboard",
			Handler: func(ctx *star.Context) *star.Response {
				role := ctx.GetMeta("auth.role") // 支持点号路径访问
				return &star.Response{Status: true, Data: role}
			},
		},
	},
})
```

## DTO 绑定与校验

Star 可自动将请求数据绑定到结构体。如果结构体实现了 `Validator`，会自动调用 `Validate()`。

```go
type Validator interface {
	Validate(ctx *star.Context) error
}
```

### Query 绑定

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

手动读取 Query：

```go
ctx.GetQuery("name")                            // string
ctx.GetQuery("age", star.StarTypeInt)           // int64
ctx.GetQueries(map[string]star.StarType{"age": star.StarTypeInt})
```

### Body 绑定

支持 `application/json`、`application/x-www-form-urlencoded` 和 `multipart/form-data`。

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

手动读取 Body：

```go
ctx.GetBody("name")   // 单个字段
ctx.GetBodyAll()      // 整个 Body 的 map
```

## 响应

Handler 返回 `*star.Response`，框架统一输出 JSON，HTTP 状态码为 200。

```go
type Response struct {
	Status  bool   `json:"status"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
	Extra   any    `json:"extra,omitempty"`
	Stack   string `json:"stack,omitempty"`
}
```

需要手动控制时，使用 Context 方法：

```go
ctx.Json(201, map[string]any{"id": 1})
ctx.Html(200, "<h1>{{.Title}}</h1>", map[string]any{"Title": "Star"})
ctx.ResponseError(400, fmt.Errorf("bad request"))
```

当 handler 返回 `nil`，或请求已被 `ctx.Json()` / `ctx.Html()` / `ctx.ResponseError()` 消费，框架不会再写第二次响应。

## 中间件

```go
type Middleware func(next star.Handler) star.Handler
```

### 全局中间件

```go
star.Use(func(next star.Handler) star.Handler {
	return func(ctx *star.Context) *star.Response {
		ctx.AddContext("start", time.Now())
		return next(ctx)
	}
})
```

### 局部中间件

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

### 内置中间件

| 中间件 | 说明 |
|--------|------|
| `RecoverMiddleware()` | panic 恢复，返回 500 |
| `RequestIdMiddleware()` | 生成 UUID 请求 ID |
| `LogMiddleware()` | 输出 HTTP 访问日志 |
| `CorsMiddleware(config ...CorsConfig)` | 设置跨域响应头，处理预检请求 |
| `RateLimitMiddleware(requests, per)` | 按 IP + Path 限流，超限返回 429 |

## 路由守卫

### 前置守卫

在匹配路由之后、DTO 绑定之前执行。返回非 `nil` 的 `*Response` 会中断后续流程。

```go
star.Use(star.BeforeRouteGuard(func(ctx *star.Context) *star.Response {
	if ctx.GetRequestHeader("Authorization") == "" {
		return &star.Response{Status: false, Message: "unauthorized"}
	}
	return nil
}))
```

### 后置守卫

在 handler 之后执行，可以替换最终响应。

```go
star.Use(star.AfterRouteGuard(func(ctx *star.Context, resp *star.Response) *star.Response {
	if resp == nil {
		return nil
	}
	resp.Extra = map[string]any{"requestId": ctx.RequestID}
	return resp
}))
```

守卫按照注册顺序执行。

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

未配置时，`CorsMiddleware()` 默认允许所有 Origin、Method 和 Header。`OPTIONS` 请求返回 `204 No Content`。

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

- `IsWebSocket: true` 时路由方法固定为 `GET`
- 非 WebSocket 握手请求返回 `400`
- 可通过 `WebsocketUpgrade` 传入自定义 `*websocket.Upgrader`

## 静态文件与 SPA

### 静态文件

```go
star.Use(star.Route{
	Path:      "/static",
	IsStatic:  true,
	StaticDir: "./static",
})
```

`GET /static/style.css` 提供 `./static/style.css`。

### SPA 回退

```go
star.Use(star.Route{
	Path:     "/app",
	IsSPA:    true,
	SPADir:   "./web",
	SPAIndex: "index.html",
})
```

如果请求路径对应的文件不存在或是目录，会回退到 `index.html`，方便前端路由接管。

## 日志与调试

开启调试模式：

```go
star.EnableDebug()
```

调试模式会输出 `DEBUG` 级别日志，500 错误响应会包含堆栈信息。

开启日志落盘：

```go
star.EnableLogSave()
```

自定义日志配置：

```go
star.EnableLogSave(star.Logger{
	FileDir: "./log",
	MaxSize: 30 * 1024, // KB；-1 表示不按大小分片
	MaxAge:  7,         // 天；-1 表示不清理
})
```

日志文件按日期命名：`2026-05-22.log`、`2026-05-22_001.log` 等。

关闭终端颜色：

```bash
NO_COLOR=1 TERM=dumb
```

## Context 参考

### 请求信息

| 方法 | 说明 |
|------|------|
| `GetPath()` | 规范化后的请求路径 |
| `GetRequestMethod()` | 原始 HTTP 方法 |
| `GetMethod()` | 命中的路由方法 |
| `GetClientIP()` | 客户端 IP 链（优先解析 `X-Forwarded-For`） |
| `GetOriginalRequest()` | 原始 `*http.Request` |
| `GetOriginalResponseWriter()` | 原始 `http.ResponseWriter` |

### 参数与模型

| 方法 | 说明 |
|------|------|
| `GetParam(key)` | 单个路径参数 |
| `GetParams()` | 全部路径参数 |
| `GetQuery(key, typ ...StarType)` | Query 值（可选类型转换） |
| `GetQueries(typMap ...map[string]StarType)` | 全部 Query 值 |
| `GetQueryModel()` | 绑定后的 Query DTO |
| `GetBody(key)` | 单个 Body 字段 |
| `GetBodyAll()` | 解析后的 Body map |
| `GetBodyModel()` | 绑定后的 Body DTO |

### 请求改写

| 方法 | 说明 |
|------|------|
| `SetQueries(map[string]string)` | 重写 Query 参数 |
| `SetBody(string)` | 重写请求体 |

### 响应控制

| 方法 | 说明 |
|------|------|
| `SetStatusCode(code)` | 设置 HTTP 状态码（仅生效一次） |
| `GetStatusCode()` | 获取当前状态码（默认 200） |
| `SetHeader(key, value)` | 设置响应头 |
| `AddHeader(key, value)` | 追加响应头 |
| `RemoveHeader(key)` | 删除响应头 |
| `GetResponseHeader(key)` | 获取响应头 |
| `GetResponseHeaders()` | 获取全部响应头 |
| `Json(status, data)` | 输出 JSON 响应 |
| `Html(status, tmpl, data...)` | 输出 HTML（模板 LRU 缓存） |
| `ResponseSuccess(response)` | 输出 200 JSON 响应 |
| `ResponseError(code, err...)` | 输出错误响应 |

### Cookie

| 方法 | 说明 |
|------|------|
| `GetCookie(key)` | 读取 Cookie |
| `GetCookies()` | 读取全部 Cookie |
| `SetCookie(key, value, maxAge)` | 设置 Cookie |
| `DeleteCookie(key)` | 删除 Cookie |

### 上下文数据与扩展

| 方法 | 说明 |
|------|------|
| `AddContext(key, value)` | 写入请求级上下文数据 |
| `GetContext(key)` | 读取上下文数据（支持 `a.b`、`items[0].id`） |
| `DelContext(key)` | 删除上下文数据 |
| `GetMeta(key)` | 读取路由元数据（支持嵌套路径） |
| `IsWebSocket()` | 判断是否是 WebSocket 握手 |
| `GetWebSocket()` | 获取 WebSocket 连接 |

## API 参考

### 全局函数

| 函数 | 说明 |
|------|------|
| `New(bind, useDefaultMiddleware...)` | 创建应用实例 |
| `Use(component)` | 注册路由、中间件或守卫 |
| `Run()` | 启动 HTTP Server |
| `EnableDebug()` | 开启调试模式 |
| `EnableLogSave(config...)` | 开启日志落盘 |
| `ConfigureCors(config)` | 注册 CORS 中间件 |

`Use()` 接受的类型：`star.Route`、`[]star.Route`、`star.Middleware`、`[]star.Middleware`、`star.BeforeRouteGuard`、`[]star.BeforeRouteGuard`、`star.AfterRouteGuard`、`[]star.AfterRouteGuard`。

### 工具函数

| 函数 | 说明 |
|------|------|
| `Now()` | 当前时间 |
| `NowFormat(format)` | 当前时间格式化 |
| `FormatTime(t, format)` | 格式化时间 |
| `ParseDateTime(s)` | 解析 `2006-01-02 15:04:05` |
| `ParseTimestamp(s)` | 解析秒级或毫秒级时间戳 |
| `VerifyTimestamp(s)` | 校验时间戳字符串 |
| `GenerateUUID()` | 生成 UUID v4 |
| `VerifyUUID(s)` | 校验 UUID |
| `GetValueByPath(obj, path)` | 从 map、struct、slice 中按路径取值 |

## License

MIT