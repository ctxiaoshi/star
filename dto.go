package star

// DTO 数据传输对象类型标记
type DTO any

// Validator 可选实现：需要绑定后校验时实现 Validate，error.Error() 为返回的错误消息。
type Validator interface {
	Validate(ctx *Context) error
}
