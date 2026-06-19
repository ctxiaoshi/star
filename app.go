package star

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

var (
	starOnce     sync.Once // 单例锁
	starInstance *Star     // 全局Star单例
	appName      string    // 应用名称
	appVersion   string    // 应用版本
	debug        bool      // 是否开启调试模式
)

// Star 框架核心结构体
type Star struct {
	bind   string       // 绑定地址
	router *Router      // 路由
	server *http.Server // 服务器
}

func init() {
	Log.I("STAR", " ███████╗████████╗ █████╗ ██████╗ ")
	Log.I("STAR", " ██╔════╝╚══██╔══╝██╔══██╗██╔══██╗")
	Log.I("STAR", " ███████╗   ██║   ███████║██████╔╝")
	Log.I("STAR", " ╚════██║   ██║   ██╔══██║██╔══██╗")
	Log.I("STAR", " ███████║   ██║   ██║  ██║██║  ██║")
	Log.I("STAR", " ╚══════╝   ╚═╝   ╚═╝  ╚═╝╚═╝  ╚═╝")
}

// New 实例化Star
//
// useDefaultMiddleware 是否使用内置中间件，默认使用
func New(bind string, useDefaultMiddleware ...bool) *Star {
	starOnce.Do(func() {
		appName = "Star"
		appVersion = "1.0.0-beta"

		starInstance = &Star{
			bind: bind,
			router: &Router{
				routes:     []Route{},
				middleware: []Middleware{},
			},
		}

		// 如果使用内置中间件
		if len(useDefaultMiddleware) == 0 || len(useDefaultMiddleware) > 0 && useDefaultMiddleware[0] {
			Use([]Middleware{
				RecoverMiddleware(),
				RequestIdMiddleware(),
				LogMiddleware(),
			})
		}
	})

	return starInstance
}

// shutdown 关闭服务器
func shutdown(srv *http.Server, done chan<- struct{}) {
	defer close(done)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	Log.I("STAR", "Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		Log.E("STAR", "Server forced shutdown: %v", err)
	}

	Log.I("STAR", "Server exited")
}

// Run 运行Star
func Run() {
	panicIfStarInstanceIsNotInitialized()

	Log.I("STAR", "Welcome to Star! Version: %s", appVersion)
	Log.I("STAR", "Run server on %s", starInstance.bind)

	starInstance.router.applyRoutes()
	starInstance.router.applyGlobalMiddleware()

	starInstance.server = &http.Server{
		Addr:         starInstance.bind,
		Handler:      ToHttpHandler(starInstance.router.root),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	done := make(chan struct{})
	go shutdown(starInstance.server, done)

	// 启动服务器
	if err := starInstance.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		Log.E("STAR", "Server error: %v", err)
		os.Exit(1)
	}

	// 等待 shutdown 完成
	<-done
}

// EnableDebug 开启调试模式
func EnableDebug() {
	debug = true
}

// EnableLogSave 启用日志保存
func EnableLogSave(config ...Logger) {
	if len(config) > 0 {
		Log.enableSave(config[0])
		return
	}
	Log.enableSave()
}

// CorsConfig 跨域配置
func ConfigureCors(config CorsConfig) {
	Use(CorsMiddleware(config))
}
