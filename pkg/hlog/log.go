package hlog

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/htime"
)

type LogFormat int

const (
	FormatColored LogFormat = iota // Default text with terminal color codes
	FormatPlain                    // Plain text without terminal color codes
	FormatJSON                     // Structural JSON format
)

type Level int

func (l Level) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	case EXIT:
		return "EXIT"
	}
	if text, ok := (*levelName.Load())[l]; ok {
		return text
	}
	return strconv.Itoa(int(l))
}

var levelNameLock sync.Mutex

func SetLevelName(level Level, name string) {
	levelNameLock.Lock()
	defer levelNameLock.Unlock()
	temp := map[Level]string{}
	m := (*levelName.Load())
	for key, val := range m {
		if val != name {
			temp[key] = val
		}
	}
	if level == DEBUG {
		name = "DEBUG"
	} else if level == INFO {
		name = "INFO"
	} else if level == WARN {
		name = "WARN"
	} else if level == ERROR {
		name = "ERROR"
	} else if level == EXIT {
		name = "EXIT"
	}
	temp[level] = name
	levelName.Store(&temp)
}

const (
	DEBUG Level = 00000
	INFO  Level = 10000
	WARN  Level = 20000
	ERROR Level = 30000
	EXIT  Level = 40000
)

var levelName atomic.Pointer[map[Level]string]

const (
	Ldate         = 1 << iota     // the date in the local time zone: 2009/01/23
	Ltime                         // the time in the local time zone: 01:23:23
	Lmicroseconds                 // microsecond resolution: 01:23:23.123123.  assumes Ltime.
	Llongfile                     // full file name and line number: /a/b/c/d.go:23
	Lshortfile                    // final file name element and line number: d.go:23. overrides Llongfile
	LUTC                          // if Ldate or Ltime is set, use UTC rather than the local time zone
	Lmsgprefix                    // move the "prefix" from the beginning of the line to before the message
	LstdFlags     = Ldate | Ltime // initial values for the standard logger
)

type Logger struct {
	lock sync.Mutex

	prefix    atomic.Pointer[string] // prefix on each line to identify the logger (but see Lmsgprefix)
	flag      atomic.Int32           // properties
	isDiscard atomic.Bool

	outErr    atomic.Bool
	outLevel  atomic.Int32
	out       map[Level]SyncWriter
	outTarget atomic.Pointer[func(level Level) SyncWriter]
	compare   func(v1 Level, v2 Level) bool

	stdout io.Writer   // 优化：生产环境直接无缓冲写入标准输出
	isJSON atomic.Bool // 优化：无反射高性能 JSON 切换开关
}

func NewLogger(prefix string, flag int) *Logger {
	l := &Logger{
		out:    map[Level]SyncWriter{},
		stdout: os.Stdout, // 直接绑定标准输出
	}
	l.SetPrefix(prefix)
	l.SetFlags(flag)
	l.SetOutLevel(ERROR)
	l.setSimple(false)
	l.setOutError(true)
	return l
}

func (l *Logger) SetPrefix(prefix string) {
	l.prefix.Store(&prefix)
}

func (l *Logger) SetFlags(flag int) {
	l.flag.Store(int32(flag))
}

func (l *Logger) SetJSON(enabled bool) {
	l.isJSON.Store(enabled)
}

func (l *Logger) Flush() {
	l.lock.Lock()
	defer l.lock.Unlock()
	// 标准输出无缓冲，仅刷新额外的文件或网络 SyncWriter
	for _, writer := range l.out {
		_ = writer.Flush()
	}
}

// Close 关闭后 Logger 不应继续使用。
func (l *Logger) Close() {
	l.Flush()
}

func (l *Logger) Debug(format string, v ...any) {
	_ = l.output(0, 2, DEBUG, func(b []byte) []byte {
		return fmt.Appendf(b, format, v...)
	}, false, "")
}

func (l *Logger) Info(format string, v ...any) {
	_ = l.output(0, 2, INFO, func(b []byte) []byte {
		return fmt.Appendf(b, format, v...)
	}, false, "")
}

func (l *Logger) Warn(format string, v ...any) {
	_ = l.output(0, 2, WARN, func(b []byte) []byte {
		return fmt.Appendf(b, format, v...)
	}, false, "")
}

func (l *Logger) Error(format string, v ...any) {
	_ = l.output(0, 2, ERROR, func(b []byte) []byte {
		return fmt.Appendf(b, format, v...)
	}, false, "")
}

func (l *Logger) Exit(format string, v ...any) {
	_ = l.output(0, 2, EXIT, func(b []byte) []byte {
		return fmt.Appendf(b, format, v...)
	}, false, "")
	panic(fmt.Sprintf(format, v...))
}

func (l *Logger) Output(calldepth int, level Level, s string) error {
	calldepth++ // +1 for this frame.
	return l.output(0, calldepth, level, func(b []byte) []byte {
		return append(b, s...)
	}, false, "")
}

// output handles the core logic of formatting and writing log records.
func (l *Logger) output(pc uintptr, calldepth int, level Level, appendOutput func([]byte) []byte, hasCtx bool, ctxVal string) error {
	if l.isDiscard.Load() {
		return nil
	}

	now := htime.NowTime()
	prefix := l.Prefix()
	flag := l.Flags()
	isJSON := l.isJSON.Load()

	var file string
	var line int
	if flag&(Lshortfile|Llongfile) != 0 {
		if pc == 0 {
			var ok bool
			_, file, line, ok = runtime.Caller(calldepth)
			if !ok {
				file = "???"
				line = 0
			}
		} else {
			fs := runtime.CallersFrames([]uintptr{pc})
			f, _ := fs.Next()
			file = f.File
			if file == "" {
				file = "???"
			}
			line = f.Line
		}
	}

	buf := getBuffer()
	defer putBuffer(buf)

	formatHeader(buf, now, prefix, flag, file, line, level, isJSON)

	if isJSON {
		// JSON 模式：将业务消息写入一个临时缓冲区进行自动化 JSON 安全转义
		msgBuf := getBuffer()
		*msgBuf = appendOutput(*msgBuf)
		if len(*msgBuf) > 0 && (*msgBuf)[len(*msgBuf)-1] == '\n' {
			*msgBuf = (*msgBuf)[:len(*msgBuf)-1]
		}
		// 追加转义后的业务消息
		appendEscapedJSONString(buf, *msgBuf)
		putBuffer(msgBuf)

		// 闭合 Context 上下文（若存在）
		if hasCtx {
			*buf = append(*buf, `,"context":"`...)
			*buf = append(*buf, ctxVal...)
			*buf = append(*buf, '"')
		}
		*buf = append(*buf, '}')
	} else {
		// 传统文本模式
		if hasCtx {
			*buf = append(*buf, '[')
			*buf = append(*buf, []byte(ctxVal)...)
			*buf = append(*buf, ']', ' ')
		}
		*buf = appendOutput(*buf)
	}

	// 统一尾部换行处理
	if len(*buf) == 0 || (*buf)[len(*buf)-1] != '\n' {
		*buf = append(*buf, '\n')
	}

	return l.writer(level, *buf)
}

func (l *Logger) writer(level Level, data []byte) error {
	// 优化点：控制台输出移出互斥锁外，解决高并发下的锁严重互斥雪崩问题
	if l.outErr.Load() {
		_, _ = l.stdout.Write(data)
	}

	if level < Level(l.outLevel.Load()) {
		return nil
	}

	// 只有低频写入外部第三方持久化 SyncWriter 时才按需短暂加锁
	l.lock.Lock()
	if _, ok := l.out[level]; !ok {
		call := l.outTarget.Load()
		if nil != call {
			temp := (*call)(level)
			temp.Compare(l.compare)
			l.out[level] = temp
		}
	}

	for _, writer := range l.out {
		_, _ = writer.Write(level, data)
	}
	l.lock.Unlock()
	return nil
}

func (l *Logger) Prefix() string {
	if p := l.prefix.Load(); p != nil {
		return *p
	}
	return ""
}

func (l *Logger) Flags() int {
	return int(l.flag.Load())
}

func (l *Logger) SetOutLevel(level Level) {
	l.outLevel.Store(int32(level))
}

func (l *Logger) GetOutLevel() Level {
	return Level(l.outLevel.Load())
}

func (l *Logger) SetOutTarget(target func(level Level) SyncWriter) {
	l.outTarget.Store(&target)
}

func (l *Logger) setOutError(err bool) {
	l.outErr.Store(err)
}

func (l *Logger) setSimple(simple bool) {
	if simple {
		l.compare = func(v1 Level, v2 Level) bool {
			return v1 == v2
		}
	} else {
		l.compare = func(v1 Level, v2 Level) bool {
			return v1 <= v2
		}
	}
	l.lock.Lock()
	for _, val := range l.out {
		val.Compare(l.compare)
	}
	l.lock.Unlock()
}

var bufferPool = sync.Pool{New: func() any { return new([]byte) }}

func getBuffer() *[]byte {
	p := bufferPool.Get().(*[]byte)
	*p = (*p)[:0]
	return p
}

func putBuffer(p *[]byte) {
	if cap(*p) > 64<<10 {
		*p = nil
	}
	bufferPool.Put(p)
}

func itoa(buf *[]byte, i int, wid int) {
	var b [20]byte
	bp := len(b) - 1
	for i >= 10 || wid > 1 {
		wid--
		q := i / 10
		b[bp] = byte('0' + i - q*10)
		bp--
		i = q
	}
	b[bp] = byte('0' + i)
	*buf = append(*buf, b[bp:]...)
}

func formatHeader(buf *[]byte, t time.Time, prefix string, flag int, file string, line int, level Level, isJSON bool) {
	if isJSON {
		// JSON 骨架高效非反射组装
		*buf = append(*buf, `{"time":`...)
		if flag&LUTC != 0 {
			t = t.UTC()
		}
		*buf = append(*buf, '"')
		*buf = t.AppendFormat(*buf, time.RFC3339Nano)
		*buf = append(*buf, `","level":"`...)
		*buf = append(*buf, level.String()...)
		*buf = append(*buf, '"')

		if prefix != "" {
			*buf = append(*buf, `,"prefix":"`...)
			*buf = append(*buf, prefix...)
			*buf = append(*buf, '"')
		}

		if flag&(Lshortfile|Llongfile) != 0 {
			if flag&Lshortfile != 0 {
				short := file
				for i := len(file) - 1; i > 0; i-- {
					if file[i] == '/' {
						short = file[i+1:]
						break
					}
				}
				file = short
			}
			*buf = append(*buf, `,"file":"`...)
			*buf = append(*buf, file...)
			*buf = append(*buf, ':')
			itoa(buf, line, -1)
			*buf = append(*buf, '"')
		}
		*buf = append(*buf, `,"msg":"`...)
		return
	}

	// 传统控制台文本带颜色模式
	if flag&Lmsgprefix == 0 {
		*buf = append(*buf, prefix...)
	}
	if flag&(Ldate|Ltime|Lmicroseconds) != 0 {
		if flag&LUTC != 0 {
			t = t.UTC()
		}
		if flag&Ldate != 0 {
			year, month, day := t.Date()
			itoa(buf, year, 4)
			*buf = append(*buf, '/')
			itoa(buf, int(month), 2)
			*buf = append(*buf, '/')
			itoa(buf, day, 2)
			*buf = append(*buf, ' ')
		}
		if flag&(Ltime|Lmicroseconds) != 0 {
			hour, min, sec := t.Clock()
			itoa(buf, hour, 2)
			*buf = append(*buf, ':')
			itoa(buf, min, 2)
			*buf = append(*buf, ':')
			itoa(buf, sec, 2)
			if flag&Lmicroseconds != 0 {
				*buf = append(*buf, '.')
				itoa(buf, t.Nanosecond()/1e3, 6)
			}
			*buf = append(*buf, ' ')
		}
	}

	if level >= EXIT {
		*buf = append(*buf, []byte("\x1b[45m")...)
	} else if level >= ERROR {
		*buf = append(*buf, []byte("\x1b[41m")...)
	} else if level >= WARN {
		*buf = append(*buf, []byte("\x1b[43m")...)
	} else if level <= DEBUG {
		*buf = append(*buf, []byte("\x1b[42m")...)
	} else {
		*buf = append(*buf, []byte("\x1b[0m")...)
	}
	*buf = append(*buf, '[')
	*buf = append(*buf, []byte(level.String())...)
	*buf = append(*buf, ']', ' ')
	*buf = append(*buf, []byte("\x1b[0m")...)
	*buf = append(*buf, ' ')

	if flag&(Lshortfile|Llongfile) != 0 {
		if flag&Lshortfile != 0 {
			short := file
			for i := len(file) - 1; i > 0; i-- {
				if file[i] == '/' {
					short = file[i+1:]
					break
				}
			}
			file = short
		}
		*buf = append(*buf, file...)
		*buf = append(*buf, ':')
		itoa(buf, line, -1)
		*buf = append(*buf, ": "...)
	}
	if flag&Lmsgprefix != 0 {
		*buf = append(*buf, prefix...)
	}
}

// appendEscapedJSONString 实现极其快速且无多余堆分配的普通字符串转义追加
func appendEscapedJSONString(buf *[]byte, s []byte) {
	for _, c := range s {
		switch c {
		case '"':
			*buf = append(*buf, '\\', '"')
		case '\\':
			*buf = append(*buf, '\\', '\\')
		case '\n':
			*buf = append(*buf, '\\', 'n')
		case '\r':
			*buf = append(*buf, '\\', 'r')
		case '\t':
			*buf = append(*buf, '\\', 't')
		default:
			*buf = append(*buf, c)
		}
	}
}

var std = NewLogger("", LstdFlags)

func init() {
	levelName.Store(&map[Level]string{
		INFO:  "INFO",
		WARN:  "WARN",
		ERROR: "ERROR",
		EXIT:  "EXIT",
		DEBUG: "DEBUG",
	})
	flag.Func("log_out", "Log output target, supports directories and HTTP upload addresses.", func(s string) error {
		if strings.Index(s, "http://") == 0 || strings.Index(s, "https://") == 0 {
			std.SetOutTarget(func(level Level) SyncWriter {
				return newHttpWriter(s, level)
			})
		} else if len(s) > 0 {
			stat, err := os.Stat(s)
			if err != nil {
				return err
			}
			if !stat.IsDir() {
				return errors.New(s + " Not a directory")
			}
			std.SetOutTarget(func(level Level) SyncWriter {
				return newFileWriter(s, level)
			})
		}
		return nil
	})
	flag.Func("log_level", "Log output level, DEBUG(00000), INFO(10000), WARN(20000), ERROR(30000)[default], EXIT(40000)", func(s string) error {
		atoi, err := strconv.Atoi(s)
		if err != nil {
			return err
		}
		std.SetOutLevel(Level(atoi))
		return nil
	})
	flag.BoolFunc("log_simple", "Enable simple mode, default is false.", func(s string) error {
		std.setSimple("true" == s)
		return nil
	})
	flag.BoolFunc("log_err", "Whether to output logs to the console, default is true.", func(s string) error {
		std.setOutError("true" == s)
		return nil
	})
	flag.BoolFunc("log_json", "Whether to output logs in structural JSON format, default is false.", func(s string) error {
		std.SetJSON("true" == s)
		return nil
	})
}
func SetFlags(flag int) {
	std.SetFlags(flag)
}
func SetPrefix(prefix string) {
	std.SetPrefix(prefix)
}
func SetJSON(enabled bool) {
	std.SetJSON(enabled)
}
func Debug(ctx context.Context, format string, v ...any) {
	_ = std.output(0, 2, DEBUG, func(b []byte) []byte {
		return fmt.Appendf(b, format, v...)
	}, true, FromContext(ctx))
}
func Info(ctx context.Context, format string, v ...any) {
	_ = std.output(0, 2, INFO, func(b []byte) []byte {
		return fmt.Appendf(b, format, v...)
	}, true, FromContext(ctx))
}
func Warn(ctx context.Context, format string, v ...any) {
	_ = std.output(0, 2, WARN, func(b []byte) []byte {
		return fmt.Appendf(b, format, v...)
	}, true, FromContext(ctx))
}
func Error(ctx context.Context, format string, v ...any) {
	_ = std.output(0, 2, ERROR, func(b []byte) []byte {
		return fmt.Appendf(b, format, v...)
	}, true, FromContext(ctx))
}
func Exit(ctx context.Context, format string, v ...any) {
	_ = std.output(0, 2, EXIT, func(b []byte) []byte {
		return fmt.Appendf(b, format, v...)
	}, true, FromContext(ctx))
	panic(fmt.Sprintf(format, v...))
}
func Output(ctx context.Context, calldepth int, level Level, s string) error {
	calldepth++
	return std.output(0, calldepth, level, func(b []byte) []byte {
		return append(b, s...)
	}, true, FromContext(ctx))
}
func GetOutLevel() Level {
	return std.GetOutLevel()
}
func Flush() {
	std.Flush()
}

type SyncWriter interface {
	Flush() error
	Sync() error
	Write(level Level, p []byte) (n int, err error)
	Compare(call func(v1 Level, v2 Level) bool)
}
