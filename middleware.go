package star

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

var (
	corsMu           sync.RWMutex
	corsAllowOrigins []string
	corsAllowSet     bool
)

// Middleware 中间件函数
type Middleware func(next Handler) Handler

// applyCorsAllowOrigins 在构建 CorsMiddleware 时同步 WebSocket CheckOrigin 的 AllowOrigins
func applyCorsAllowOrigins(origins []string) {
	corsMu.Lock()
	defer corsMu.Unlock()
	corsAllowOrigins = append([]string(nil), origins...)
	corsAllowSet = true
}

// MatchCORSRequestOrigin 判断请求 Origin 是否命中当前 CORS 的 AllowOrigins
func MatchCORSRequestOrigin(r *http.Request) bool {
	corsMu.RLock()
	defer corsMu.RUnlock()
	if !corsAllowSet {
		return true
	}
	if slices.Contains(corsAllowOrigins, "*") {
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	return slices.Contains(corsAllowOrigins, origin)
}

// RequestIdMiddleware 请求 ID 中间件
func RequestIdMiddleware() Middleware {
	return func(next Handler) Handler {
		return func(ctx *Context) *Response {
			ctx.RequestID = GenerateUUID()
			ctx.SetHeader("Request-ID", ctx.RequestID)
			return next(ctx)
		}
	}
}

// LogMiddleware 日志中间件，记录请求方法、路径、状态码和耗时
func LogMiddleware() Middleware {
	return func(next Handler) Handler {
		return func(ctx *Context) *Response {
			start := time.Now()
			resp := next(ctx)
			duration := time.Since(start)
			httpCode := ctx.GetStatusCode()
			ip := strings.Join(ctx.GetClientIP(), ",")
			requestId := ctx.RequestID

			var pattern = "%s | %d | %s | %s | %s | %v | %s"
			args := []any{ctx.GetRequestMethod(), httpCode, ctx.GetPath(), ip, requestId, duration, ctx.r.UserAgent()}

			switch {
			case httpCode == http.StatusTooManyRequests:
				pattern += " | %s(%d/%vs)"
				args = append(args, "RATE LIMIT", ctx.rateLimitInfo.requests, ctx.rateLimitInfo.per.Seconds())
				Log.W("HTTP", pattern, args...)
			case httpCode == http.StatusNotFound || httpCode == http.StatusInternalServerError:
				Log.E("HTTP", pattern, args...)
			case duration > 3*time.Second:
				pattern += " | %s(> %v)"
				args = append(args, "SLOW", duration)
				Log.W("HTTP", pattern, args...)
			case httpCode >= http.StatusOK && httpCode < http.StatusMultipleChoices:
				Log.I("HTTP", pattern, args...)
			default:
				if ctx.err != nil {
					pattern += " | %s"
					args = append(args, ctx.err.Error())
				}
				Log.W("HTTP", pattern, args...)
			}

			return resp
		}
	}
}

// CorsConfig 跨域配置
type CorsConfig struct {
	AllowOrigins     []string // 允许的源，默认 ["*"]
	AllowMethods     []string // 允许的方法，默认 ["GET","POST","PUT","DELETE","OPTIONS","HEAD"]
	AllowHeaders     []string // 允许的请求头，默认 ["Content-Type","Authorization"]
	ExposeHeaders    []string // 暴露的响应头
	AllowCredentials bool     // 是否允许携带凭证
	MaxAge           int      // 预检请求缓存时间（秒），默认 86400
}

// CorsMiddleware 跨域中间件
func CorsMiddleware(config ...CorsConfig) Middleware {
	cfg := CorsConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"*"},
		AllowHeaders: []string{"*"},
		MaxAge:       86400,
	}
	if len(config) > 0 {
		c := config[0]
		if len(c.AllowOrigins) > 0 {
			cfg.AllowOrigins = c.AllowOrigins
		}
		if len(c.AllowMethods) > 0 {
			cfg.AllowMethods = c.AllowMethods
		}
		if len(c.AllowHeaders) > 0 {
			cfg.AllowHeaders = c.AllowHeaders
		}
		if len(c.ExposeHeaders) > 0 {
			cfg.ExposeHeaders = c.ExposeHeaders
		}
		cfg.AllowCredentials = c.AllowCredentials
		if c.MaxAge > 0 {
			cfg.MaxAge = c.MaxAge
		}
	}

	origins := strings.Join(cfg.AllowOrigins, ", ")
	methods := strings.Join(cfg.AllowMethods, ", ")
	headers := strings.Join(cfg.AllowHeaders, ", ")
	expose := strings.Join(cfg.ExposeHeaders, ", ")
	maxAge := fmt.Sprintf("%d", cfg.MaxAge)

	applyCorsAllowOrigins(cfg.AllowOrigins)

	return func(next Handler) Handler {
		return func(ctx *Context) *Response {
			ctx.SetHeader("Access-Control-Allow-Origin", origins)
			ctx.SetHeader("Access-Control-Allow-Methods", methods)
			ctx.SetHeader("Access-Control-Allow-Headers", headers)
			ctx.SetHeader("Access-Control-Max-Age", maxAge)
			if expose != "" {
				ctx.SetHeader("Access-Control-Expose-Headers", expose)
			}
			if cfg.AllowCredentials {
				ctx.SetHeader("Access-Control-Allow-Credentials", "true")
			}

			if ctx.GetRequestMethod() == OPTIONS {
				ctx.SetStatusCode(http.StatusNoContent)
				return nil
			}

			return next(ctx)
		}
	}
}

// RecoverMiddleware 异常恢复中间件
func RecoverMiddleware() Middleware {
	return func(next Handler) Handler {
		return func(ctx *Context) (resp *Response) {
			defer func() {
				if rec := recover(); rec != nil {
					Log.E("STAR", "panic recovered: %v", rec)
					var err error
					switch x := rec.(type) {
					case error:
						err = x
					default:
						err = fmt.Errorf("%v", x)
					}
					ctx.ResponseError(http.StatusInternalServerError, err)
					resp = nil
				}
			}()
			return next(ctx)
		}
	}
}

// RateLimitMiddleware 限流中间件
func RateLimitMiddleware(requests int, per time.Duration) Middleware {
	// 使用互斥锁保护 map
	var mu sync.Mutex
	// 记录每个 IP+路径 的请求信息
	clients := make(map[string][]time.Time)
	// 清理过期记录
	go func() {
		for {
			time.Sleep(per)
			mu.Lock()
			for key, times := range clients {
				var active []time.Time
				cutoff := time.Now().Add(-per)
				for _, t := range times {
					if t.After(cutoff) {
						active = append(active, t)
					}
				}

				if len(active) == 0 {
					delete(clients, key)
				} else {
					clients[key] = active
				}
			}
			mu.Unlock()
		}
	}()

	return func(next Handler) Handler {
		return func(ctx *Context) *Response {
			// 获取客户端 IP 和请求路径
			ip := ctx.GetClientIP()
			path := ctx.GetPath()
			// 创建组合键：IP + 路径
			key := strings.Join(ip, ":") + ":" + path

			mu.Lock()
			// 获取该 IP+路径 的请求记录
			times, exists := clients[key]
			if !exists {
				times = []time.Time{}
				clients[key] = times
			}
			// 清除超过限制时间的记录
			cutoff := time.Now().Add(-per)
			var active []time.Time
			for _, t := range times {
				if t.After(cutoff) {
					active = append(active, t)
				}
			}
			clients[key] = active
			// 检查是否超过速率限制
			if len(active) >= requests {
				mu.Unlock()
				ctx.rateLimitInfo.requests = len(active)
				ctx.rateLimitInfo.per = per
				ctx.rateLimitInfo.lastTime = time.Now()
				ctx.ResponseError(http.StatusTooManyRequests)
				return nil
			}
			// 记录新的请求时间
			clients[key] = append(active, time.Now())
			mu.Unlock()

			return next(ctx)
		}
	}
}
