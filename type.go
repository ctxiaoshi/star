package star

type StarType string

const (
	StarTypeString StarType = "str"   // 字符串类型 => string
	StarTypeInt    StarType = "int"   // 整数类型 => int64
	StarTypeFloat  StarType = "float" // 浮点数类型 => float64
	StarTypeBool   StarType = "bool"  // 布尔类型 => bool
	StarTypeTime   StarType = "time"  // 时间类型 => time.Time
)
