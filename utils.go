package star

import (
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// panicIfStarInstanceIsNotInitialized 如果Star实例未初始化，则panic
func panicIfStarInstanceIsNotInitialized() {
	if starInstance == nil {
		Log.E("STAR", "Star is not initialized, call New() first")
		os.Exit(1)
	}
}

// captureStack 获取当前堆栈信息
func captureStack() string {
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	lines := strings.Split(string(buf[:n]), "\n")
	// 跳过 captureStack 和 InternalErrorPage 自身的帧
	skip := 0
	for i, line := range lines {
		if strings.Contains(line, "runtime.Stack") {
			skip = i + 2
			break
		}
	}
	if skip > 0 && skip < len(lines) {
		lines = lines[skip:]
	}
	return strings.Join(lines, "\n")
}

// pathToken 路径片段：字段名或下标
type pathToken struct {
	key   string
	isIdx bool
	idx   int
}

func parseValuePath(path string) ([]pathToken, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, false
	}
	var tokens []pathToken
	i := 0
	for i < len(path) {
		switch path[i] {
		case '.':
			i++
		case '[':
			end := strings.IndexByte(path[i:], ']')
			if end < 0 {
				return nil, false
			}
			end += i
			n, err := strconv.Atoi(strings.TrimSpace(path[i+1 : end]))
			if err != nil || n < 0 {
				return nil, false
			}
			tokens = append(tokens, pathToken{isIdx: true, idx: n})
			i = end + 1
		default:
			j := i
			for j < len(path) && path[j] != '.' && path[j] != '[' {
				j++
			}
			if j == i {
				return nil, false
			}
			tokens = append(tokens, pathToken{key: path[i:j]})
			i = j
		}
	}
	if len(tokens) == 0 {
		return nil, false
	}
	return tokens, true
}

func derefReflect(v reflect.Value) reflect.Value {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	return v
}

func reflectGetKey(v reflect.Value, key string) reflect.Value {
	v = derefReflect(v)
	if !v.IsValid() {
		return reflect.Value{}
	}
	switch v.Kind() {
	case reflect.Map:
		kt := v.Type().Key()
		kv := reflect.ValueOf(key)
		if kv.Type().AssignableTo(kt) {
			// ok
		} else if kv.Type().ConvertibleTo(kt) {
			kv = kv.Convert(kt)
		} else {
			return reflect.Value{}
		}
		mv := v.MapIndex(kv)
		if !mv.IsValid() {
			return reflect.Value{}
		}
		return mv
	case reflect.Struct:
		return structFieldByPathKey(v, key)
	default:
		return reflect.Value{}
	}
}

func structFieldByPathKey(v reflect.Value, key string) reflect.Value {
	t := v.Type()
	if sf, ok := t.FieldByName(key); ok && sf.IsExported() {
		return v.FieldByIndex(sf.Index)
	}
	if len(key) > 0 {
		title := strings.ToUpper(key[:1]) + key[1:]
		if sf, ok := t.FieldByName(title); ok && sf.IsExported() {
			return v.FieldByIndex(sf.Index)
		}
	}
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		if sf.Anonymous {
			inner := v.Field(i)
			if r := structFieldByPathKey(inner, key); r.IsValid() {
				return r
			}
			continue
		}
		jtag := sf.Tag.Get("json")
		if c := strings.IndexByte(jtag, ','); c >= 0 {
			jtag = jtag[:c]
		}
		if jtag == key || strings.EqualFold(sf.Name, key) {
			return v.Field(i)
		}
	}
	return reflect.Value{}
}

func reflectGetIndex(v reflect.Value, idx int) reflect.Value {
	v = derefReflect(v)
	if !v.IsValid() {
		return reflect.Value{}
	}
	switch v.Kind() {
	case reflect.Slice, reflect.Array:
		if idx < 0 || idx >= v.Len() {
			return reflect.Value{}
		}
		return v.Index(idx)
	default:
		return reflect.Value{}
	}
}

// getValueByPath 按路径取值，支持 map、struct、slice/array；路径如 a.b、items[0].id
// 不存在或无法导出为 any 时返回 nil。
func GetValueByPath(obj any, path string) any {
	tokens, ok := parseValuePath(path)
	if !ok {
		return nil
	}
	v := reflect.ValueOf(obj)
	v = derefReflect(v)
	if !v.IsValid() {
		return nil
	}
	for _, tok := range tokens {
		if tok.isIdx {
			v = reflectGetIndex(v, tok.idx)
		} else {
			v = reflectGetKey(v, tok.key)
		}
		v = derefReflect(v)
		if !v.IsValid() {
			return nil
		}
	}
	if !v.CanInterface() {
		return nil
	}
	return v.Interface()
}

// GenerateUUID 生成 UUID
func GenerateUUID() string {
	return uuid.NewString()
}

// VerifyUUID 验证 UUID
func VerifyUUID(u string) bool {
	if strings.TrimSpace(u) == "" {
		return false
	}
	return uuid.Validate(u) == nil
}
