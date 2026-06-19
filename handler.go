package star

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// Method 请求方法
type Method string

const (
	GET     Method = "GET"     // GET请求
	POST    Method = "POST"    // POST请求
	PUT     Method = "PUT"     // PUT请求
	DELETE  Method = "DELETE"  // DELETE请求
	PATCH   Method = "PATCH"   // PATCH请求
	OPTIONS Method = "OPTIONS" // OPTIONS请求
	HEAD    Method = "HEAD"    // HEAD请求
	ANY     Method = "ANY"     // 所有请求
)

// Param 路径参数结构体
type Param struct {
	Key      string   // 参数名
	Value    any      // 参数值
	Type     StarType // 参数类型
	Default  any      // 默认值
	Optional bool     // 是否可选
}

// Response 响应结构体
type Response struct {
	Status  bool   `json:"status"`          // 状态
	Message string `json:"message"`         // 消息
	Data    any    `json:"data,omitempty"`  // 数据
	Extra   any    `json:"extra,omitempty"` // 额外数据，用于扩展响应内容，比如 {"show": true, "layer": "alert"}，可以用于控制前端是否显示响应内容，或者显示的弹窗类型等
	Stack   string `json:"stack,omitempty"` // 堆栈跟踪，只在 debug 模式下返回
}

// Handler 处理函数
type Handler func(ctx *Context) *Response

// ToHttpHandler 转换为 http.HandlerFunc
func ToHttpHandler(handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := &Context{
			w: w,
			r: r,
		}

		response := handler(ctx)

		// 如果 response 为 nil 或请求已消费，则不返回响应
		if response == nil || ctx.requestConsumed {
			return
		}

		// 这里指响应请求成功，并非业务成功，业务成功与否应由 handler 返回的 Response 的 Status 决定
		ctx.ResponseSuccess(response)
	}
}

// wrapWebSocketHandler 升级为 WebSocket 连接
func wrapWebSocketHandler(up *websocket.Upgrader, handler Handler) Handler {
	return func(ctx *Context) *Response {
		if !ctx.IsWebSocket() {
			ctx.ResponseError(http.StatusBadRequest)
			return nil
		}

		u := up
		if u == nil {
			u = &websocket.Upgrader{
				CheckOrigin: MatchCORSRequestOrigin,
			}
		}
		conn, err := u.Upgrade(ctx.w, ctx.r, nil)
		if err != nil {
			return nil
		}
		ctx.ws = conn
		handler(ctx)
		return nil
	}
}

// onlyFilesFS 禁止目录浏览：目录请求直接返回错误
type onlyFilesFS struct {
	root http.Dir
}

func (fs onlyFilesFS) Open(name string) (http.File, error) {
	f, err := fs.root.Open(name)
	if err != nil {
		return nil, err
	}
	stat, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if stat.IsDir() {
		f.Close()
		return nil, os.ErrNotExist
	}
	return f, nil
}

// wrapStaticHandler 静态文件服务
func wrapStaticHandler(dir, prefix string, allowDir bool) Handler {
	if dir == "" {
		dir = "./static"
	}
	var fs http.FileSystem = http.Dir(dir)
	if !allowDir {
		fs = onlyFilesFS{root: http.Dir(dir)}
	}
	return func(ctx *Context) *Response {
		http.StripPrefix(prefix, http.FileServer(fs)).ServeHTTP(ctx.w, ctx.r)
		return nil
	}
}

// wrapSPAHandler SPA 模式：文件存在则直接返回，否则回退到索引文件
func wrapSPAHandler(dir, index string) Handler {
	if dir == "" {
		dir = "./web"
	}
	if index == "" {
		index = "index.html"
	}
	root := http.Dir(dir)
	fs := http.FileServer(root)

	return func(ctx *Context) *Response {
		p := ctx.r.URL.Path
		f, err := root.Open(p)
		if err != nil {
			http.ServeFile(ctx.w, ctx.r, filepath.Join(dir, index))
			return nil
		}
		fi, err := f.Stat()
		f.Close()
		if err != nil || fi.IsDir() {
			http.ServeFile(ctx.w, ctx.r, filepath.Join(dir, index))
			return nil
		}
		fs.ServeHTTP(ctx.w, ctx.r)
		return nil
	}
}

// defaultHandler 默认主页处理器
func defaultHandler(ctx *Context) *Response {
	tmpl, data := HelloWorldPage()
	ctx.Html(http.StatusOK, tmpl, data)
	return nil
}

// pathHasPrefix 检查 reqParts 是否以 patParts 为前缀（段级别）
func pathHasPrefix(reqParts, patParts []string) bool {
	if len(reqParts) < len(patParts) {
		return false
	}
	for i, p := range patParts {
		if reqParts[i] != p {
			return false
		}
	}
	return true
}

// matchPathPattern 将请求路径段与路由模式匹配，成功则返回路径参数 map。
func matchPathPattern(patParts, reqParts []string) (map[string]Param, bool) {
	requiredCount := 0
	for _, pat := range patParts {
		if strings.HasPrefix(pat, "{") && strings.HasSuffix(pat, "}") {
			inner := pat[1 : len(pat)-1]
			_, _, _, opt := parseParamDef(inner)
			if !opt {
				requiredCount++
			}
		} else {
			requiredCount++
		}
	}
	if len(reqParts) < requiredCount || len(reqParts) > len(patParts) {
		return nil, false
	}

	params := make(map[string]Param)
	for i, pat := range patParts {
		if strings.HasPrefix(pat, "{") && strings.HasSuffix(pat, "}") {
			inner := pat[1 : len(pat)-1]
			name, typ, defVal, optional := parseParamDef(inner)
			if i < len(reqParts) {
				val, err := convertParam(reqParts[i], typ)
				if err != nil {
					return nil, false
				}
				params[name] = Param{Key: name, Value: val, Type: typ, Default: defVal, Optional: optional}
			} else if defVal != nil {
				params[name] = Param{Key: name, Value: defVal, Type: typ, Default: defVal, Optional: optional}
			} else {
				return nil, false
			}
		} else if i >= len(reqParts) || pat != reqParts[i] {
			return nil, false
		}
	}
	return params, true
}

// coerceMapValues 遍历 map，将可解析为数字的 string 转为 int64 / float64，解决 query 参数均为字符串的问题。
func coerceMapValues(m map[string]any) map[string]any {
	for k, v := range m {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			m[k] = n
		} else if f, err := strconv.ParseFloat(s, 64); err == nil {
			m[k] = f
		}
	}
	return m
}

// bindRouteInput 绑定并校验路由上的 Query / Body DTO；失败时已写入错误响应，返回 false。
func bindRouteInput(ctx *Context, h *parsedRoute) bool {
	if h.query != nil {
		bound, err := bindDTOFromMap(h.query, coerceMapValues(ctx.GetQueries()))
		if err != nil {
			ctx.ResponseError(http.StatusBadRequest)
			return false
		}
		ctx.query = bound
		if err := runDTOValidate(ctx, ctx.query); err != nil {
			ctx.ResponseError(http.StatusBadRequest, err)
			return false
		}
	}
	if h.body != nil {
		raw := ctx.GetBodyAll()
		// 如果 raw 为 nil，则创建一个空的 map[string]any，用于触发 Validate 方法
		if raw == nil {
			raw = make(map[string]any)
		}
		bound, err := bindDTOFromMap(h.body, raw)
		if err != nil {
			ctx.ResponseError(http.StatusBadRequest)
			return false
		}
		ctx.body = bound
		if err := runDTOValidate(ctx, ctx.body); err != nil {
			ctx.ResponseError(http.StatusBadRequest, err)
			return false
		}
	}
	return true
}

func runBeforeRouteGuards(ctx *Context, guards []BeforeRouteGuard) (resp *Response, stop bool) {
	for _, guard := range guards {
		if resp := guard(ctx); resp != nil {
			return resp, true
		}
		if ctx.requestConsumed {
			return nil, true
		}
	}
	return nil, false
}

func runAfterRouteGuards(ctx *Context, guards []AfterRouteGuard, handlerResp *Response) (resp *Response, stop bool) {
	for _, guard := range guards {
		if resp := guard(ctx, handlerResp); resp != nil {
			return resp, true
		}
		if ctx.requestConsumed {
			return nil, true
		}
	}
	return nil, false
}

// rootHandler 根处理器
func (r *Router) rootHandler(ctx *Context) *Response {
	reqParts := splitPath(ctx.GetPath())
	method := ctx.GetRequestMethod()
	pathMatched := false

	for pattern, h := range r.parsedRoutes {
		patMethod, patPath := parsePatternKey(pattern)
		patParts := splitPath(patPath)
		var params map[string]Param
		var ok bool
		if h.isPrefix {
			if !pathHasPrefix(reqParts, patParts) {
				continue
			}
			ok = true
			params = make(map[string]Param)
		} else {
			params, ok = matchPathPattern(patParts, reqParts)
		}
		if !ok {
			continue
		}
		pathMatched = true
		if patMethod != ANY && patMethod != method {
			continue
		}

		ctx.params = params
		ctx.method = h.method

		if h.meta != nil {
			ctx.meta = h.meta
		}
		if resp, stop := runBeforeRouteGuards(ctx, r.BeforeHandler); stop {
			return resp
		}
		// 在 beforeHandler 之后解析并绑定DTO，这样可以在 beforeHandler 中对参数做前置处理，比如对加密的参数进行解密
		if !bindRouteInput(ctx, &h) {
			return nil
		}
		response := h.handler(ctx)
		if resp, stop := runAfterRouteGuards(ctx, r.AfterHandler, response); stop {
			return resp
		}
		return response
	}

	if pathMatched {
		ctx.ResponseError(http.StatusMethodNotAllowed)
	} else {
		ctx.ResponseError(http.StatusNotFound)
	}
	return nil
}

// bindDTOFromMap 将 map[string]any 按 proto 类型 JSON 对齐解码
func bindDTOFromMap(proto DTO, data map[string]any) (any, error) {
	if proto == nil {
		return nil, nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	rv := reflect.ValueOf(proto)
	if !rv.IsValid() {
		return nil, nil
	}
	switch rv.Kind() {
	case reflect.Pointer:
		if rv.IsNil() {
			return nil, fmt.Errorf("model is nil pointer")
		}
		out := reflect.New(rv.Elem().Type())
		if err := json.Unmarshal(raw, out.Interface()); err != nil {
			return nil, err
		}
		return out.Interface(), nil
	default:
		out := reflect.New(rv.Type())
		if err := json.Unmarshal(raw, out.Interface()); err != nil {
			return nil, err
		}
		return out.Elem().Interface(), nil
	}
}

// runDTOValidate 若 v 实现 Validator 则调用 Validate
func runDTOValidate(ctx *Context, v any) error {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer && rv.IsNil() {
		return nil
	}
	if z, ok := v.(Validator); ok {
		return z.Validate(ctx)
	}
	if rv.Kind() == reflect.Pointer {
		return nil
	}
	p := reflect.New(rv.Type())
	p.Elem().Set(rv)
	if z, ok := p.Interface().(Validator); ok {
		return z.Validate(ctx)
	}
	return nil
}

// parsePatternKey 从 "METHOD /path" 格式中拆分方法和路径
func parsePatternKey(key string) (method Method, path string) {
	before, after, ok := strings.Cut(key, " ")
	if !ok {
		return "", key
	}
	return Method(before), after
}

// validParamTypes 有效的路径参数类型
var validParamTypes = map[StarType]bool{
	StarTypeString: true,
	StarTypeInt:    true,
	StarTypeFloat:  true,
	StarTypeBool:   true,
	StarTypeTime:   true,
}

// parseParamDef 解析路径参数定义
//
// 支持格式：
//
//	{name}             → str, 无默认值, 必选
//	{name?}            → 非法（可选必须有默认值）
//	{name:int}         → int, 无默认值, 必选
//	{name:int:1}       → int, 默认值 1, 必选
//	{name?:int:1}      → int, 默认值 1, 可选
//	{name:hello}       → str, 默认值 "hello"（第二段不是合法类型，视为 str 的默认值）
//	{name?:hello}      → str, 默认值 "hello", 可选
func parseParamDef(inner string) (name string, typ StarType, defVal any, optional bool) {
	parts := strings.SplitN(inner, ":", 3)
	name = parts[0]
	typ = "str"

	if strings.HasSuffix(name, "?") {
		name = name[:len(name)-1]
		optional = true
	}

	// len(parts) == 1: {name}
	if len(parts) == 1 {
		return
	}

	// len(parts) == 2: {name:type} / {name:default}
	if len(parts) == 2 {
		if validParamTypes[StarType(parts[1])] {
			typ = StarType(parts[1])
		} else {
			defVal = parts[1]
		}
		return
	}

	// len(parts) == 3: {name:type:default}
	if validParamTypes[StarType(parts[1])] {
		typ = StarType(parts[1])
		defVal, _ = convertParam(parts[2], typ)
	} else {
		defVal = parts[1] + ":" + parts[2]
	}
	return
}

// convertParam 将字符串按类型转换
func convertParam(raw string, typ StarType) (any, error) {
	switch typ {
	case StarTypeString:
		return raw, nil
	case StarTypeInt:
		return strconv.ParseInt(raw, 10, 64)
	case StarTypeFloat:
		return strconv.ParseFloat(raw, 64)
	case StarTypeBool:
		switch raw {
		case "true", "1":
			return true, nil
		case "false", "0":
			return false, nil
		default:
			return nil, fmt.Errorf("invalid bool: %s", raw)
		}
	case StarTypeTime:
		if ts, err := strconv.ParseInt(raw, 10, 64); err == nil {
			if ts > 1e12 {
				return time.UnixMilli(ts), nil
			}
			return time.Unix(ts, 0), nil
		}
		for _, layout := range []string{time.RFC3339, string(DateFormatYMDHMS), string(DateFormatYMD)} {
			if t, err := time.Parse(layout, raw); err == nil {
				return t, nil
			}
		}
		return nil, fmt.Errorf("invalid time: %s", raw)
	default:
		return nil, fmt.Errorf("unknown type: %s", typ)
	}
}
