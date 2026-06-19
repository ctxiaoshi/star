package star

import (
	"maps"
	"strings"

	"github.com/gorilla/websocket"
)

// route 路由条目结构体
type parsedRoute struct {
	method   Method         // 请求方法
	handler  Handler        // 处理函数
	query    DTO            // 查询参数绑定模型
	body     DTO            // 请求体绑定模型
	meta     map[string]any // 元数据
	isPrefix bool           // 前缀匹配（静态文件/SPA）
}

// Route 路由结构体
type Route struct {
	// 请求方法
	//
	// 若为空，在子路由上会继承最近祖先已指定的方法；根级仍为空时视为 ANY。
	Method Method
	// 路径
	//
	// 支持路径参数定义，支持类型声明，默认类型为 str
	//
	// 支持类型：
	//   str: 字符串类型，映射为 string
	//   int: 整数类型，映射为 int64
	//   float: 浮点数类型，映射为 float64
	//   bool: 布尔类型，接受 true 或 false、1 或 0，映射为 bool
	//   time: 时间类型，接受日期时间字符串、时间戳， 映射为 time.Time
	//
	// 支持可选参数，使用 ? 表示可选
	// 如果是可选参数，则必须有默认值，并且可选参数必须放在必选参数的后面
	// 例如：
	//   /user/{id}
	//   /list/{page?:int:1}/{size?:int:10}
	Path string
	// 查询参数绑定的模型
	//
	// 如果模型实现 Validator 接口，则会在处理请求前进行参数验证
	Query DTO
	// 请求体绑定的模型
	//
	// 如果模型实现 Validator 接口，则会在处理请求前进行参数验证
	Body DTO
	// 处理函数
	Handler Handler
	// 局部中间件
	Middleware []Middleware
	// 子路由
	Children []Route
	// 是否是 WebSocket 路由
	IsWebSocket bool
	// WebSocket 升级器
	//
	// 当 IsWebSocket 为 true 时，可设置 WebSocket 升级器，否则使用默认升级器
	WebsocketUpgrade *websocket.Upgrader
	// 是否是静态文件路由
	IsStatic bool
	// 静态文件目录
	//
	// 当 IsStatic 为 true 时，可设置静态文件目录，否则使用默认目录 ./static
	StaticDir string
	// 是否允许目录遍历
	//
	// 当 IsStatic 为 true 时，可设置是否允许目录访问，默认不允许
	AllowDir bool
	// 是否是 SPA 路由
	IsSPA bool
	// SPA 路由目录
	//
	// 当 IsSPA 为 true 时，可设置 SPA 路由目录，否则使用默认目录 ./web
	SPADir string
	// SPA 路由索引文件
	//
	// 当 IsSPA 为 true 时，可设置 SPA 路由索引文件，否则使用默认文件 index.html
	SPAIndex string
	// 元数据
	//
	// 通过 Meta 可以传递一些元数据到路由处理函数中，在路由处理函数中可以通过 ctx.GetMeta(key) 获取，支持多级获取，不存在则返回 nil。
	// 子路由会与祖先路由的 Meta 做浅合并，同名键以本路由（子）为准。
	Meta map[string]any
}

// BeforeRouteGuard 路由前置守卫。返回 nil 表示继续执行下一个守卫或进入路由 handler；返回非 nil 则立即作为 HTTP 响应并终止（后续守卫与 handler 均不再执行）。
type BeforeRouteGuard func(ctx *Context) *Response

// AfterRouteGuard 路由后置守卫。入参 response 为路由 handler 的返回值；返回 nil 表示继续下一个守卫；返回非 nil 则作为最终响应并终止（后续 After 守卫不再执行）。
type AfterRouteGuard func(ctx *Context, response *Response) *Response

// Router 路由器结构体
type Router struct {
	middleware    []Middleware           // 全局中间件
	routes        []Route                // 路由表
	root          Handler                // 根处理器
	parsedRoutes  map[string]parsedRoute // 解析后的路由表
	BeforeHandler []BeforeRouteGuard     // 前置守卫链，按切片下标顺序依次执行
	AfterHandler  []AfterRouteGuard      // 后置守卫链，按切片下标顺序依次执行
}

// useRoutes 应用路由
func (r *Router) useRoutes(routes []Route) {
	r.routes = append(r.routes, routes...)
	r.root = r.rootHandler
}

// applyRoutes 应用路由
func (r *Router) applyRoutes() {
	if r.root == nil || len(r.routes) == 0 {
		Log.W("STAR", "No routes registered, using default handler")
		r.root = defaultHandler
		return
	}

	r.parseRoute()
}

// useMiddleware 应用中间件
func (r *Router) useMiddleware(middleware []Middleware) {
	r.middleware = append(r.middleware, middleware...)
}

// applyGlobalMiddleware 将全局中间件包裹到根处理器上
func (r *Router) applyGlobalMiddleware() {
	for i := len(r.middleware) - 1; i >= 0; i-- {
		r.root = r.middleware[i](r.root)
	}
}

// useBeforeHandler 向前置守卫链末尾追加一项（与 star.BeforeHandler 注册顺序一致：先注册的先执行）。
func (r *Router) useBeforeHandler(guards ...BeforeRouteGuard) {
	for _, guard := range guards {
		r.BeforeHandler = append(r.BeforeHandler, guard)
	}
}

// useAfterHandler 向后置守卫链末尾追加一项（与 star.AfterHandler 注册顺序一致：先注册的先执行）。
func (r *Router) useAfterHandler(guards ...AfterRouteGuard) {
	for _, guard := range guards {
		r.AfterHandler = append(r.AfterHandler, guard)
	}
}

// routeEntry 用于收集展平后的路由条目
type routeEntry struct {
	method string
	path   string
}

// parseRoute 解析路由
func (r *Router) parseRoute() {
	if r.parsedRoutes == nil {
		r.parsedRoutes = make(map[string]parsedRoute)
	}

	var entries []routeEntry
	r.flatten("", r.routes, nil, nil, "", &entries)

	pathWidth := len("Path")
	for _, e := range entries {
		if len(e.path) > pathWidth {
			pathWidth = len(e.path)
		}
	}

	line := "+-" + strings.Repeat("-", 7) + "-+-" + strings.Repeat("-", pathWidth) + "-+"
	Log.D("ROUTE", "%s", line)
	Log.D("ROUTE", "| %-7s | %-*s |", "Method", pathWidth, "Path")
	Log.D("ROUTE", "%s", line)
	for _, e := range entries {
		Log.D("ROUTE", "| %-7s | %-*s |", e.method, pathWidth, e.path)
	}
	Log.D("ROUTE", "%s", line)
}

// mergeRouteMeta 浅合并 Meta：先父后子，同名键以 child 为准；皆空返回 nil。
func mergeRouteMeta(parent, child map[string]any) map[string]any {
	if len(parent) == 0 && len(child) == 0 {
		return nil
	}
	out := make(map[string]any, len(parent)+len(child))
	maps.Copy(out, parent)
	maps.Copy(out, child)
	return out
}

// resolveRouteMethod 得到本节点注册用的 HTTP 方法：WebSocket/静态/SPA 固定为 GET；
// 本节点已指定 Method 则用本节点；否则若 parentMethod 非空则继承父级；否则 ANY。
func resolveRouteMethod(route Route, parentMethod Method) Method {
	if route.IsWebSocket || route.IsStatic || route.IsSPA {
		return GET
	}
	if strings.TrimSpace(string(route.Method)) != "" {
		return route.Method
	}
	if strings.TrimSpace(string(parentMethod)) != "" {
		return parentMethod
	}
	return ANY
}

// flatten 递归展开子路由，拼接路径前缀并注册。
// parentMethod 为祖先链上已解析的方法，供未指定 Method 的子路由继承。
func (r *Router) flatten(prefix string, routes []Route, parentMW []Middleware, parentMeta map[string]any, parentMethod Method, entries *[]routeEntry) {
	for _, route := range routes {
		method := resolveRouteMethod(route, parentMethod)

		fullPath := normalizePath(prefix + "/" + route.Path)

		// 累积中间件：父级 + 当前路由
		mw := make([]Middleware, 0, len(parentMW)+len(route.Middleware))
		mw = append(mw, parentMW...)
		mw = append(mw, route.Middleware...)

		mergedMeta := mergeRouteMeta(parentMeta, route.Meta)

		var handler Handler
		if route.IsWebSocket {
			handler = wrapWebSocketHandler(route.WebsocketUpgrade, route.Handler)
		} else if route.IsStatic {
			handler = wrapStaticHandler(route.StaticDir, fullPath, route.AllowDir)
		} else if route.IsSPA {
			handler = wrapSPAHandler(route.SPADir, route.SPAIndex)
		} else if route.Handler != nil {
			handler = route.Handler
		}
		if handler != nil {
			for i := len(mw) - 1; i >= 0; i-- {
				handler = mw[i](handler)
			}
			key := string(method) + " " + fullPath
			r.parsedRoutes[key] = parsedRoute{method: method, handler: handler, query: route.Query, body: route.Body, meta: mergedMeta, isPrefix: route.IsStatic || route.IsSPA}
			*entries = append(*entries, routeEntry{method: string(method), path: fullPath})
		}

		if len(route.Children) > 0 {
			r.flatten(fullPath, route.Children, mw, mergedMeta, method, entries)
		}
	}
}

// normalizePath 规范化路径，去除多余斜杠
func normalizePath(path string) string {
	parts := make([]string, 0)
	for _, p := range splitPath(path) {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return "/" + strings.Join(parts, "/")
}

// splitPath 分割路径
func splitPath(path string) []string {
	result := make([]string, 0)
	seg := ""
	for _, c := range path {
		if c == '/' {
			if seg != "" {
				result = append(result, seg)
				seg = ""
			}
		} else {
			seg += string(c)
		}
	}
	if seg != "" {
		result = append(result, seg)
	}
	return result
}
