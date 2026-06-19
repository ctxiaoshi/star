package star

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	colorReset  = "\033[0m"
	colorDim    = "\033[90m"
	colorTag    = "\033[96m" // 亮青，标签
	colorInfo   = "\033[32m" // 绿
	colorDebug  = "\033[36m" // 青
	colorWarn   = "\033[33m" // 黄
	colorError  = "\033[31m" // 红
	colorBright = "\033[1m"
)

// logger 内部日志实现
type logger struct {
	mu          sync.Mutex
	save        bool
	fileDir     string
	maxSize     int // KB，单日单分片上限；-1 不限制
	maxAge      int // 天，-1 不按日期清理
	file        *os.File
	fileDay     string // 当前打开文件所属日期 2006-01-02
	fileShard   int    // 0=当天主文件 YYYY-MM-DD.log；≥1 为分片 YYYY-MM-DD_NNN.log
	written     int64
	cleanupOnce sync.Once
}

// Logger 日志落盘配置
//
// 文件命名：2006-01-02_001.log、2006-01-02_002.log … 按天、按大小分片。
type Logger struct {
	Save    bool   // 是否保存日志
	FileDir string // 日志目录，默认 ./log
	MaxSize int    // 单日单分片最大 KB，默认约 30MB，超限同一天递增 _001；-1 表示当天仅一个文件、不按大小分片
	MaxAge  int    // 按文件名中的日期保留天数，默认 7；-1 不清理
}

// logNoColor 终端是否禁用颜色
func logNoColor() bool {
	return os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb"
}

func levelColorCode(levelLetter string) string {
	switch levelLetter {
	case "I":
		return colorInfo
	case "D":
		return colorDebug
	case "W":
		return colorWarn
	case "E":
		return colorError
	default:
		return ""
	}
}

// basePrint 基础打印
func (l *logger) basePrint(level string, tag, message string, args ...any) {
	body := fmt.Sprintf(message, args...)
	now := NowFormat(DateFormatYMDHMSM)

	levelLetter := " "
	if level != "" {
		levelLetter = strings.ToUpper(level[0:1])
	}

	plain := fmt.Sprintf("[%s] [%s] [%s] %s", now, levelLetter, tag, body)

	var dim, lc, tagC, reset, bright string
	if !logNoColor() {
		dim = colorDim
		lc = levelColorCode(levelLetter)
		if lc == "" {
			lc = colorDim
		}
		tagC = colorTag
		reset = colorReset
		bright = colorBright
	}

	line := fmt.Sprintf("%s[%s]%s %s%s[%s]%s %s[%s]%s %s%s%s",
		dim, now, reset,
		lc, bright, levelLetter, reset,
		tagC, tag, reset,
		lc, body, reset)

	fmt.Println(line)

	if l.save {
		l.appendPlain(plain)
	}
}

// I 打印 INFO
func (l *logger) I(tag, message string, args ...any) {
	l.basePrint("INFO", tag, message, args...)
}

// D 打印 DEBUG；仅调试模式
func (l *logger) D(tag, message string, args ...any) {
	if !debug {
		return
	}
	l.basePrint("DEBUG", tag, message, args...)
}

// W 打印 WARN
func (l *logger) W(tag, message string, args ...any) {
	l.basePrint("WARN", tag, message, args...)
}

// E 打印 ERROR
func (l *logger) E(tag, message string, args ...any) {
	l.basePrint("ERROR", tag, message, args...)
}

// EnableSave 启用日志保存
func (l *logger) enableSave(config ...Logger) {
	l.mu.Lock()
	l.save = true
	if len(config) > 0 {
		c := config[0]
		if c.FileDir != "" {
			l.fileDir = c.FileDir
		}
		if c.MaxSize != 0 {
			l.maxSize = c.MaxSize
		}
		if c.MaxAge != 0 {
			l.maxAge = c.MaxAge
		}
	}
	_ = os.MkdirAll(l.fileDir, 0755)
	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}
	l.fileDay = ""
	l.fileShard = 0
	_ = l.pickLogFileForTodayLocked()
	l.mu.Unlock()

	l.cleanupOnce.Do(func() {
		go l.cleanupLoop()
	})
}

// logPathFor 当天主文件为 day.log；分片为 day_001.log、day_002.log …
func logPathFor(dir, day string, shard int) string {
	if shard == 0 {
		return filepath.Join(dir, day+".log")
	}
	return filepath.Join(dir, fmt.Sprintf("%s_%03d.log", day, shard))
}

// pickLogFileForTodayLocked 选择当天应写入的文件
func (l *logger) pickLogFileForTodayLocked() error {
	day := time.Now().Format("2006-01-02")

	// 未限制大小：仅 YYYY-MM-DD.log
	if l.maxSize < 0 {
		return l.openLogTargetLocked(day, 0)
	}

	limit := int64(l.maxSize) * 1024
	if limit <= 0 {
		return l.openLogTargetLocked(day, 0)
	}

	primaryPath := logPathFor(l.fileDir, day, 0)
	st, err := os.Stat(primaryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return l.openLogTargetLocked(day, 0)
		}
		return err
	}
	if st.Size() < limit {
		return l.openLogTargetLocked(day, 0)
	}

	entries, err := os.ReadDir(l.fileDir)
	if err != nil {
		return l.openLogTargetLocked(day, 1)
	}

	prefix := day + "_"
	maxShard := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".log") || !strings.HasPrefix(name, prefix) {
			continue
		}
		rest := strings.TrimSuffix(name[len(prefix):], ".log")
		s, err := strconv.Atoi(rest)
		if err != nil || s < 1 {
			continue
		}
		if s > maxShard {
			maxShard = s
		}
	}

	if maxShard == 0 {
		return l.openLogTargetLocked(day, 1)
	}

	lastPath := logPathFor(l.fileDir, day, maxShard)
	st2, err := os.Stat(lastPath)
	if err != nil {
		return l.openLogTargetLocked(day, 1)
	}
	if st2.Size() < limit {
		return l.openLogTargetLocked(day, maxShard)
	}
	return l.openLogTargetLocked(day, maxShard+1)
}

// openLogTargetLocked shard=0 为主日志 day.log，shard≥1 为 day_NNN.log
func (l *logger) openLogTargetLocked(day string, shard int) error {
	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}
	path := logPathFor(l.fileDir, day, shard)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	l.file = f
	l.fileDay = day
	l.fileShard = shard
	l.written = st.Size()
	return nil
}

// appendPlain 追加日志到文件
func (l *logger) appendPlain(plain string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.save {
		return
	}

	today := time.Now().Format("2006-01-02")
	if l.file == nil || l.fileDay != today {
		if err := l.pickLogFileForTodayLocked(); err != nil {
			return
		}
	}

	data := []byte(plain + "\n")
	if l.maxSize >= 0 {
		limit := int64(l.maxSize) * 1024
		if limit > 0 && l.written+int64(len(data)) > limit {
			_ = l.openLogTargetLocked(l.fileDay, l.fileShard+1)
		}
	}

	if l.file == nil {
		if err := l.pickLogFileForTodayLocked(); err != nil {
			return
		}
	}

	n, err := l.file.Write(data)
	if err == nil {
		l.written += int64(n)
	}
}

// cleanupLoop 定时清理过期日志
func (l *logger) cleanupLoop() {
	l.purgeOldLogs()
	ticker := time.NewTicker(24 * time.Hour)
	for range ticker.C {
		l.purgeOldLogs()
	}
}

// parseLogFileDate 从 2006-01-02.log 或 2006-01-02_001.log 解析日志日期
func parseLogFileDate(name string) (time.Time, bool) {
	if !strings.HasSuffix(name, ".log") {
		return time.Time{}, false
	}
	base := strings.TrimSuffix(name, ".log")
	var dateStr string
	switch {
	case len(base) == 10:
		dateStr = base
	case len(base) > 10 && base[10] == '_':
		dateStr = base[:10]
	default:
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02", dateStr, time.Local)
	return t, err == nil
}

// purgeOldLogs 清理过期日志
func (l *logger) purgeOldLogs() {
	l.mu.Lock()
	dir := l.fileDir
	maxAge := l.maxAge
	l.mu.Unlock()

	if maxAge < 0 {
		return
	}
	now := time.Now()
	cutoff := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -maxAge)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		logDate, ok := parseLogFileDate(e.Name())
		if !ok {
			continue
		}
		if logDate.Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// Log 全局日志实例
var Log = &logger{
	save:    false,
	fileDir: "./log",
	maxSize: 30 * 1024,
	maxAge:  7,
}
