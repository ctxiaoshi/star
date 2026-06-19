package star

import (
	"strconv"
	"strings"
	"time"
)

type DateFormat string

const (
	DateFormatYMDHMS  = "2006-01-02 15:04:05"     // 年-月-日 时:分:秒
	DateFormatYMD     = "2006-01-02"              // 年-月-日
	DateFormatHMS     = "15:04:05"                // 时:分:秒
	DateFormatYMDHMSM = "2006-01-02 15:04:05.000" // 年-月-日 时:分:秒.毫秒
)

// FormatTime 格式化时间
func FormatTime(t time.Time, format DateFormat) string {
	return t.Format(string(format))
}

// Now 获取当前时间
func Now() time.Time {
	return time.Now()
}

// NowFormat 获取当前时间并格式化
func NowFormat(format DateFormat) string {
	return FormatTime(Now(), format)
}

// ParseDateTime 按 DateFormatYMDHMS 解析日期时间字符串
func ParseDateTime(t string) (time.Time, error) {
	return time.Parse(DateFormatYMDHMS, t)
}

// ParseTimestamp 将时间戳字符串解析为时间
// 支持秒和毫秒两种时间戳格式
func ParseTimestamp(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	if n >= 1_000_000_000_000 {
		return time.UnixMilli(n), nil
	}
	return time.Unix(n, 0), nil
}

// VerifyTimestamp 校验时间戳
func VerifyTimestamp(t string) bool {
	s := strings.TrimSpace(t)
	if s == "" {
		return false
	}
	_, err := strconv.ParseInt(s, 10, 64)
	return err == nil
}
