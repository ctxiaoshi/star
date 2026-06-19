package star

import (
	"bytes"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// defaultMultipartMemory 为 ParseMultipartForm 的内存上限（超出部分写入临时文件）
const defaultMultipartMemory = 32 << 20

// pageTemplateCacheMax 页面模板缓存上限；超出时按 LRU 淘汰最久未使用的条目。
const pageTemplateCacheMax = 100

var (
	pageTemplateMu    sync.Mutex
	pageTemplateCache = make(map[string]*template.Template)
	// pageTemplateLRU 与 map 同步：下标 0 为最久未使用（优先淘汰），末尾为最近使用。
	pageTemplateLRU []string
)

// templateCacheTouchLocked 将 key 记为「刚用过」：若在 pageTemplateLRU 里则先删掉再接到切片末尾（MRU）。
// 须已持有 pageTemplateMu，且 key 应与 pageTemplateCache 中的条目一致。
func templateCacheTouchLocked(key string) {
	for i, k := range pageTemplateLRU {
		if k == key {
			pageTemplateLRU = append(pageTemplateLRU[:i], pageTemplateLRU[i+1:]...)
			break
		}
	}
	pageTemplateLRU = append(pageTemplateLRU, key)
}

// templateCachePutLocked 登记新解析的模板：写入 map，并把 key 接到 LRU 末尾。
// 若 key 已存在（并发双检）则只更新 *Template 并 touch；超过 pageTemplateCacheMax 时从 LRU 头部淘汰并 delete map。
// 须已持有 pageTemplateMu。
func templateCachePutLocked(key string, t *template.Template) {
	if _, exists := pageTemplateCache[key]; exists {
		pageTemplateCache[key] = t
		templateCacheTouchLocked(key)
		return
	}
	pageTemplateCache[key] = t
	pageTemplateLRU = append(pageTemplateLRU, key)
	for len(pageTemplateLRU) > pageTemplateCacheMax {
		evict := pageTemplateLRU[0]
		pageTemplateLRU = pageTemplateLRU[1:]
		delete(pageTemplateCache, evict)
	}
}

// Context 上下文结构体
type Context struct {
	// 请求ID
	RequestID string
	// 原始HTTP请求
	r *http.Request
	// 原始HTTP响应
	w http.ResponseWriter
	// 原始ws连接
	ws *websocket.Conn
	// 路径参数
	//
	// 例如：
	//   /user/{id}
	//       => params["id"]   = Param{Key: "id", Value: ""}
	//
	//   /list/{page:int:1}/{size:int:10}
	//       => params["page"] = Param{Key: "page", Value: 1}
	//       => params["size"] = Param{Key: "size", Value: 10}
	params map[string]Param
	// 查询参数
	query any
	// 请求体
	body any
	// 请求方式
	method Method
	// 元数据
	meta map[string]any
	// 上下文数据
	ctxData map[string]any
	// 是否已消费请求
	requestConsumed bool
	// 请求失败时的错误信息
	err error
	// 限速信息
	rateLimitInfo struct {
		requests int           // 请求次数
		per      time.Duration // 时间周期
		lastTime time.Time     // 上次请求时间
	}
	// 是否已设置 HTTP 状态码
	statusSet bool
	// HTTP 状态码
	statusCode int
	// 请求体解析结果
	bodyParsed map[string]any
	// 请求体解析锁
	bodyOnce sync.Once
}

// GetPath 获取请求路径
func (c *Context) GetPath() string {
	return normalizePath(c.r.URL.Path)
}

// GetMethod 获取预置的请求方式
func (c *Context) GetMethod() Method {
	return c.method
}

// GetRequestMethod 获取请求方式
func (c *Context) GetRequestMethod() Method {
	return Method(strings.ToUpper(c.r.Method))
}

// GetParam 获取路径参数值
func (c *Context) GetParam(key string) any {
	if strings.TrimSpace(key) == "" {
		return nil
	}

	if p, ok := c.params[key]; ok {
		return p.Value
	}
	return nil
}

// GetParams 获取所有路径参数
func (c *Context) GetParams() map[string]any {
	params := make(map[string]any)
	for k, v := range c.params {
		params[k] = v.Value
	}
	return params
}

// GetQuery 获取查询参数值
func (c *Context) GetQuery(key string, typ ...StarType) any {
	if strings.TrimSpace(key) == "" {
		return nil
	}

	query := c.r.URL.Query().Get(key)
	if len(typ) > 0 {
		v, _ := convertParam(query, typ[0])
		return v
	}

	return query
}

// GetQueries 获取所有查询参数（每个键只取第一个值）。
// 若传入 typMap，则对其中出现的键按 StarType 转换，与路径参数 convertParam 一致；转换失败则为 nil；未在 typMap 中的键仍为 string。
func (c *Context) GetQueries(typMap ...map[string]StarType) map[string]any {
	q := c.r.URL.Query()
	out := make(map[string]any, len(q))
	var tm map[string]StarType
	if len(typMap) > 0 {
		tm = typMap[0]
	}
	for k, vals := range q {
		raw := ""
		if len(vals) > 0 {
			raw = vals[0]
		}
		if tm != nil {
			if typ, ok := tm[k]; ok {
				v, err := convertParam(raw, typ)
				if err != nil {
					out[k] = nil
				} else {
					out[k] = v
				}
				continue
			}
		}
		out[k] = raw
	}
	return out
}

// SetQueries 设置查询参数
// 该方法用于重写查询参数
func (c *Context) SetQueries(query map[string]string) {
	values := url.Values{}
	for k, v := range query {
		values.Set(k, v)
	}
	c.r.URL.RawQuery = values.Encode()
}

// SetBody 设置请求体
// 该方法用于重写请求体
func (c *Context) SetBody(body string) {
	c.bodyOnce = sync.Once{}
	c.bodyParsed = nil
	if c.r == nil {
		return
	}
	c.r.Body = io.NopCloser(strings.NewReader(body))
	c.r.ContentLength = int64(len(body))
	c.r.Header.Set("Content-Length", strconv.Itoa(len(body)))
}

// GetQueryModel 返回路由上 Route.Query 绑定后的模型
func (c *Context) GetQueryModel() any {
	return c.query
}

// GetBodyModel 返回路由上 Route.Body 绑定后的模型
func (c *Context) GetBodyModel() any {
	return c.body
}

// GetBody 获取请求体
func (c *Context) GetBody(key string) any {
	if c.r.Body == nil || strings.TrimSpace(key) == "" {
		return nil
	}

	jsonData := c.GetBodyAll()
	if jsonData == nil {
		return nil
	}

	if v, ok := jsonData[key]; ok {
		return v
	}

	return nil
}

// GetBodyAll 获取所有请求体
func (c *Context) GetBodyAll() map[string]any {
	c.bodyOnce.Do(func() {
		c.bodyParsed = c.loadBodyParsed()
	})
	return c.bodyParsed
}

// loadBodyParsed 解析请求体
func (c *Context) loadBodyParsed() map[string]any {
	if c.r == nil || c.r.Body == nil {
		return nil
	}

	body, err := io.ReadAll(c.r.Body)
	if cerr := c.r.Body.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		return nil
	}

	c.r.Body = io.NopCloser(bytes.NewBuffer(body))

	var jsonData map[string]any

	bodyType := c.GetRequestHeader("Content-Type")
	if strings.Contains(bodyType, "application/json") {
		if err := json.Unmarshal(body, &jsonData); err != nil {
			return nil
		}
	} else if strings.Contains(bodyType, "application/x-www-form-urlencoded") {
		vals, err := url.ParseQuery(string(body))
		if err != nil {
			return nil
		}
		jsonData = make(map[string]any, len(vals))
		for k, vv := range vals {
			if len(vv) == 0 {
				jsonData[k] = ""
			} else {
				jsonData[k] = vv[0]
			}
		}
	} else if strings.Contains(bodyType, "multipart/form-data") {
		if err := c.r.ParseMultipartForm(defaultMultipartMemory); err != nil {
			return nil
		}
		mf := c.r.MultipartForm
		if mf == nil {
			return nil
		}
		jsonData = make(map[string]any, len(mf.Value)+len(mf.File))
		for k, vv := range mf.Value {
			if len(vv) == 0 {
				jsonData[k] = ""
			} else {
				jsonData[k] = vv[0]
			}
		}
		for k, files := range mf.File {
			if len(files) == 0 {
				continue
			}
			if len(files) == 1 {
				jsonData[k] = files[0]
			} else {
				jsonData[k] = files
			}
		}
	} else {
		return nil
	}

	return jsonData
}

// AddResponseHeader 添加响应头
func (c *Context) AddHeader(key, value string) {
	c.w.Header().Add(key, value)
}

// SetResponseHeader 设置响应头
func (c *Context) SetHeader(key, value string) {
	c.w.Header().Set(key, value)
}

// RemoveResponseHeader 删除响应头
func (c *Context) RemoveHeader(key string) {
	c.w.Header().Del(key)
}

// GetResponseHeader 获取响应头
func (c *Context) GetResponseHeader(key string) string {
	return c.w.Header().Get(key)
}

// GetResponseHeaders 获取所有响应头
func (c *Context) GetResponseHeaders() http.Header {
	return c.w.Header()
}

// GetRequestHeader 获取请求头
func (c *Context) GetRequestHeader(key string) string {
	return c.r.Header.Get(key)
}

// GetRequestHeaders 获取所有请求头
func (c *Context) GetRequestHeaders() http.Header {
	return c.r.Header
}

// GetCookie 获取Cookie
func (c *Context) GetCookie(key string) string {
	cookie, err := c.r.Cookie(key)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// GetCookies 获取所有Cookie
func (c *Context) GetCookies() []map[string]string {
	cookies := c.r.Cookies()
	cookieMap := make([]map[string]string, len(cookies))
	for i, cookie := range cookies {
		cookieMap[i] = map[string]string{
			cookie.Name: cookie.Value,
		}
	}
	return cookieMap
}

// SetCookie 设置Cookie
// key: 键
// value: 值
// maxAge: 有效期（秒）
func (c *Context) SetCookie(key, value string, maxAge int) {
	cookie := &http.Cookie{
		Name:     key,
		Value:    value,
		MaxAge:   maxAge,
		Path:     "/",
		HttpOnly: true,
		Secure:   c.r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(c.w, cookie)
}

// DeleteCookie 删除Cookie
func (c *Context) DeleteCookie(key string) {
	cookie := &http.Cookie{
		Name:     key,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   c.r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(c.w, cookie)
}

// SetStatusCode 设置响应状态码
func (c *Context) SetStatusCode(code int) {
	// 如果已经设置过状态码，则不重复设置
	if c.statusSet {
		return
	}
	c.statusSet = true
	c.statusCode = code
	c.w.WriteHeader(code)
}

// GetStatusCode 获取将写入的 HTTP 状态码
func (c *Context) GetStatusCode() int {
	if c.statusSet {
		return c.statusCode
	}
	return http.StatusOK
}

// GetClientIP 获取客户端 IP 链
//
// 存在 X-Forwarded-For 时，按逗号拆分并 trim，顺序为客户端在前、代理在后。
// 否则依次为 X-Real-IP、CF-Connecting-IP、RemoteAddr。
func (c *Context) GetClientIP() []string {
	if xff := c.r.Header.Get("X-Forwarded-For"); xff != "" {
		var ips []string
		for part := range strings.SplitSeq(xff, ",") {
			ip := strings.TrimSpace(part)
			if ip != "" {
				ips = append(ips, ip)
			}
		}
		if len(ips) > 0 {
			return ips
		}
	}
	if xri := strings.TrimSpace(c.r.Header.Get("X-Real-IP")); xri != "" {
		return []string{xri}
	}
	if cf := strings.TrimSpace(c.r.Header.Get("CF-Connecting-IP")); cf != "" {
		return []string{cf}
	}
	host := c.r.RemoteAddr
	if h, _, err := net.SplitHostPort(c.r.RemoteAddr); err == nil {
		host = h
	}
	return []string{host}
}

// IsWebSocket 是否是 WebSocket 握手请求
func (c *Context) IsWebSocket() bool {
	if c.GetRequestMethod() != GET {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(c.GetRequestHeader("Upgrade")), "websocket") {
		return false
	}
	conn := strings.ToLower(c.GetRequestHeader("Connection"))
	if !strings.Contains(conn, "upgrade") {
		return false
	}
	return c.GetRequestHeader("Sec-WebSocket-Key") != "" && c.GetRequestHeader("Sec-WebSocket-Version") != ""
}

// GetMeta 获取元数据
func (c *Context) GetMeta(key string) any {
	if c.meta == nil {
		return nil
	}

	return GetValueByPath(c.meta, key)
}

// AddContext 添加上下文数据
func (c *Context) AddContext(key string, value any) {
	if c.ctxData == nil {
		c.ctxData = make(map[string]any)
	}
	c.ctxData[key] = value
}

// GetContext 获取上下文数据
func (c *Context) GetContext(key string) any {
	if c.ctxData == nil {
		return nil
	}
	return GetValueByPath(c.ctxData, key)
}

// DelContext 删除上下文数据
func (c *Context) DelContext(key string) {
	if c.ctxData == nil {
		return
	}
	delete(c.ctxData, key)
}

// Json 响应JSON数据
func (c *Context) Json(status int, data any) {
	if data == nil {
		return
	}

	buf := &bytes.Buffer{}
	encoder := json.NewEncoder(buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(data); err != nil {
		c.ResponseError(http.StatusInternalServerError, err)
		return
	}

	c.SetHeader("Content-Type", "application/json; charset=utf-8")
	c.SetStatusCode(status)
	c.w.Write(buf.Bytes())
}

// Html 响应HTML数据
func (c *Context) Html(status int, tmpl string, data ...map[string]any) {
	var t *template.Template

	pageTemplateMu.Lock()
	if cached, ok := pageTemplateCache[tmpl]; ok {
		t = cached
		templateCacheTouchLocked(tmpl)
		pageTemplateMu.Unlock()
	} else {
		pageTemplateMu.Unlock()
		parsed, err := template.New("html").Parse(tmpl)
		pageTemplateMu.Lock()
		if t2, ok2 := pageTemplateCache[tmpl]; ok2 {
			t = t2
			templateCacheTouchLocked(tmpl)
			pageTemplateMu.Unlock()
		} else if err != nil {
			pageTemplateMu.Unlock()
			c.ResponseError(http.StatusInternalServerError, err)
			return
		} else {
			templateCachePutLocked(tmpl, parsed)
			t = parsed
			pageTemplateMu.Unlock()
		}
	}

	buf := &bytes.Buffer{}
	if len(data) > 0 {
		if err := t.Execute(buf, data[0]); err != nil {
			c.ResponseError(http.StatusInternalServerError, err)
			return
		}
	}

	c.SetHeader("Content-Type", "text/html; charset=utf-8")
	c.SetStatusCode(status)
	c.w.Write(buf.Bytes())
}

// ResponseError 按请求 Content-Type 返回 JSON 或 HTML 错误页（debug 下带 Stack）
func (c *Context) ResponseError(code int, err ...error) {
	c.requestConsumed = true

	if len(err) == 0 {
		err = []error{errors.New(http.StatusText(code))}
	}

	c.err = err[0]

	contentType := c.GetRequestHeader("Content-Type")

	if strings.Contains(contentType, "application/json") ||
		strings.Contains(contentType, "application/x-www-form-urlencoded") ||
		strings.Contains(contentType, "multipart/form-data") {
		response := &Response{Message: err[0].Error()}
		if debug && code == http.StatusInternalServerError {
			errorStack := captureStack()
			response.Stack = errorStack
			Log.E("ERROR", "Error: %s.\nStack: %s", err[0].Error(), errorStack)
		}
		c.Json(code, response)
	} else {
		tmpl, data := autoErrorPage(code, err[0])
		c.Html(code, tmpl, data)
	}
}

// ResponseSuccess 响应成功
func (c *Context) ResponseSuccess(response *Response) {
	c.requestConsumed = true

	c.Json(http.StatusOK, response)
}

// GetOriginalResponseWriter 获取原始响应写入器
func (c *Context) GetOriginalResponseWriter() http.ResponseWriter {
	return c.w
}

// GetOriginalRequest 获取原始请求
func (c *Context) GetOriginalRequest() *http.Request {
	return c.r
}

// GetWebSocket 获取 WebSocket 连接
func (c *Context) GetWebSocket() *websocket.Conn {
	return c.ws
}
