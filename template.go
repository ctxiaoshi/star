package star

import (
	"fmt"
	"net/http"
)

const errorPageTmpl = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>{{.Code}} - {{.Title}}</title>
<style>
  *{margin:0;padding:0;box-sizing:border-box}
  body{
    min-height:100vh;display:flex;align-items:center;justify-content:center;
    font-family:"Courier New",Consolas,monospace;
    background:#fff;color:#333;
  }
  .code{font-size:72px;font-weight:700;color:#222;line-height:1}
  .title{margin-top:.5rem;font-size:16px;color:#666}
  .msg{margin-top:1rem;font-size:13px;color:#999;line-height:1.6}
  .card{text-align:center}
  .stack{margin-top:1.5rem;padding:1rem;background:#f5f5f5;border-radius:4px;
    text-align:left;font-size:12px;color:#c00;line-height:1.8;
    max-width:800px;overflow-x:auto;white-space:pre}
</style>
</head>
<body>
<div class="card">
  <div class="code">{{.Code}}</div>
  <div class="title">{{.Title}}</div>
  <div class="msg">{{.Message}}</div>
  {{if .Stack}}<pre class="stack">{{.Stack}}</pre>{{end}}
</div>
</body>
</html>`

// errorPage 错误页面
func errorPage(code int, title, message string) (string, map[string]any) {
	return errorPageTmpl, map[string]any{
		"Code":    fmt.Sprintf("%d", code),
		"Title":   title,
		"Message": message,
	}
}

// NotFoundPage 404 页面
func NotFoundPage() (string, map[string]any) {
	return errorPage(404, "Not Found", "The page you are looking for does not exist.")
}

// InternalErrorPage 500 页面
//
// 可选参数 err 用于在 debug 模式下显示错误信息和堆栈
func InternalErrorPage(err ...any) (string, map[string]any) {
	tmpl, data := errorPage(500, "Internal Server Error", "Something went wrong on our end.")
	if debug && len(err) > 0 {
		stack := fmt.Sprintf("panic: %v\n\n%s", err[0], captureStack())
		data["Stack"] = stack
		Log.E("ERROR", "Error: %s.\nStack: %s", err[0], stack)
	}
	return tmpl, data
}

// BadRequestPage 400 页面
func BadRequestPage() (string, map[string]any) {
	return errorPage(400, "Bad Request", "The request could not be understood by the server.")
}

// UnauthorizedPage 401 页面
func UnauthorizedPage() (string, map[string]any) {
	return errorPage(401, "Unauthorized", "You need to authenticate to access this resource.")
}

// ForbiddenPage 403 页面
func ForbiddenPage() (string, map[string]any) {
	return errorPage(403, "Forbidden", "You do not have permission to access this resource.")
}

// MethodNotAllowedPage 405 页面
func MethodNotAllowedPage() (string, map[string]any) {
	return errorPage(405, "Method Not Allowed", "The request method is not supported for this resource.")
}

// autoErrorPage 自动选择错误页面
func autoErrorPage(code int, err ...any) (string, map[string]any) {
	switch code {
	case http.StatusNotFound:
		return NotFoundPage()
	case http.StatusInternalServerError:
		return InternalErrorPage(err...)
	case http.StatusBadRequest:
		return BadRequestPage()
	case http.StatusUnauthorized:
		return UnauthorizedPage()
	case http.StatusForbidden:
		return ForbiddenPage()
	case http.StatusMethodNotAllowed:
		return MethodNotAllowedPage()
	default:
		return errorPage(code, "Error", "An error occurred.")
	}
}

// HelloWorld 默认主页模板
func HelloWorldPage() (string, map[string]any) {
	tmpl := `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>{{.Name}}</title>
<style>
  *{margin:0;padding:0;box-sizing:border-box}
  body{
    min-height:100vh;display:flex;align-items:center;justify-content:center;
    font-family:"Courier New",Consolas,monospace;
    background:#fff;color:#333;
  }
  pre{font-size:14px;line-height:1.2;color:#222;font-family:'Courier New', monospace, 'DejaVu Sans Mono';}
  .info{margin-top:1rem;font-size:13px;color:#888;line-height:1.8}
  .info span{color:#333}
</style>
</head>
<body>
<div>
<pre>
 ____  _
/ ___|| |_ __ _ _ __
\___ \| __/ _` + "`" + ` | '__|
___) | || (_| | |
|____/ \__\__,_|_|
</pre>
<div class="info">
  :: <span>{{.Name}}</span> ::&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;(v{{.Version}})<br>
  A lightweight, fast and elegant Go web framework.
</div>
</div>
</body>
</html>`
	data := map[string]any{
		"Name":    appName,
		"Version": appVersion,
	}
	return tmpl, data
}
