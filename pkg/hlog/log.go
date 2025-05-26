package hlog

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Level int

func (l Level) String() string {
	if text, ok := (*levelName.Load())[l]; ok {
		return text
	}
	return strconv.Itoa(int(l))
}

func SetLevelName(level Level, name string) {
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
}

func NewLogger(prefix string, flag int) *Logger {
	l := &Logger{
		out: map[Level]SyncWriter{},
	}
	l.SetPrefix(prefix)
	l.SetFlags(flag)
	l.SetOutLevel(ERROR)
	l.setSimple(false)
	l.setOutError(true)
	go l.flushDaemon()
	return l
}

func (l *Logger) SetPrefix(prefix string) {
	l.prefix.Store(&prefix)
}

func (l *Logger) SetFlags(flag int) {
	l.flag.Store(int32(flag))
}

func (l *Logger) Flush() {
	l.lock.Lock()
	defer l.lock.Unlock()
	for _, writer := range l.out {
		_ = writer.Flush()
	}
}
func (l *Logger) Debug(v ...any) {
	_ = l.output(0, 2, DEBUG, func(b []byte) []byte {
		return fmt.Append(b, v...)
	})
}
func (l *Logger) Info(v ...any) {
	_ = l.output(0, 2, INFO, func(b []byte) []byte {
		return fmt.Append(b, v...)
	})
}

func (l *Logger) Warn(v ...any) {
	_ = l.output(0, 2, WARN, func(b []byte) []byte {
		return fmt.Append(b, v...)
	})
}

func (l *Logger) Error(v ...any) {
	_ = l.output(0, 2, ERROR, func(b []byte) []byte {
		return fmt.Append(b, v...)
	})
}

func (l *Logger) Exit(v ...any) {
	_ = l.output(0, 2, EXIT, func(b []byte) []byte {
		return fmt.Append(b, v...)
	})
	panic(fmt.Sprint(v...))
}

func (l *Logger) Output(calldepth int, level Level, s string) error {
	calldepth++ // +1 for this frame.
	return l.output(0, calldepth, level, func(b []byte) []byte {
		return append(b, s...)
	})
}

// output can take either a calldepth or a pc to get source line information.
// It uses the pc if it is non-zero.
func (l *Logger) output(pc uintptr, calldepth int, level Level, appendOutput func([]byte) []byte) error {
	if l.isDiscard.Load() {
		return nil
	}

	now := time.Now() // get this early.

	// Load prefix and flag once so that their value is consistent within
	// this call regardless of any concurrent changes to their value.
	prefix := l.Prefix()
	flag := l.Flags()

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

	formatHeader(buf, now, prefix, flag, file, line, level)

	*buf = appendOutput(*buf)
	if len(*buf) == 0 || (*buf)[len(*buf)-1] != '\n' {
		*buf = append(*buf, '\n')
	}

	return l.writer(level, *buf)
}

func (l *Logger) writer(level Level, data []byte) error {
	l.lock.Lock()
	defer l.lock.Unlock()
	if l.outErr.Load() {
		os.Stderr.Write(data)
	}

	if level < Level(l.outLevel.Load()) {
		return nil
	}
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

func (l *Logger) flushDaemon() {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			l.Flush()
			//case sev := <-s.flushChan:
			//	s.flush(sev)
		}
	}
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
			return v1 < v2
		}
	}
	for _, val := range l.out {
		val.Compare(l.compare)
	}
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
	// Assemble decimal in reverse order.
	var b [20]byte
	bp := len(b) - 1
	for i >= 10 || wid > 1 {
		wid--
		q := i / 10
		b[bp] = byte('0' + i - q*10)
		bp--
		i = q
	}
	// i < 10
	b[bp] = byte('0' + i)
	*buf = append(*buf, b[bp:]...)
}

// formatHeader writes log header to buf in following order:
//   - l.prefix (if it's not blank and Lmsgprefix is unset),
//   - date and/or time (if corresponding flags are provided),
//   - file and line number (if corresponding flags are provided),
//   - l.prefix (if it's not blank and Lmsgprefix is set).
func formatHeader(buf *[]byte, t time.Time, prefix string, flag int, file string, line int, level Level) {
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
	*buf = append(*buf, ']')
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
	flag.BoolFunc("log_simple", "Enable simple mode, default is false. In simple mode, the log file contains only logs of the current level. In complex mode, the log file includes logs of lower levels as well.", func(s string) error {
		std.setSimple("true" == s)
		return nil
	})
	flag.BoolFunc("log_err", "Whether to output logs to the console, default is true.", func(s string) error {
		std.setOutError("true" == s)
		return nil
	})
}

func SetFlags(flag int) {
	std.SetFlags(flag)
}

func SetPrefix(prefix string) {
	std.SetPrefix(prefix)
}

func Debug(v ...any) {
	_ = std.output(0, 2, DEBUG, func(b []byte) []byte {
		return fmt.Append(b, v...)
	})
}

func Info(v ...any) {
	_ = std.output(0, 2, INFO, func(b []byte) []byte {
		return fmt.Append(b, v...)
	})
}

func Warn(v ...any) {
	_ = std.output(0, 2, WARN, func(b []byte) []byte {
		return fmt.Append(b, v...)
	})
}

func Error(v ...any) {
	_ = std.output(0, 2, ERROR, func(b []byte) []byte {
		return fmt.Append(b, v...)
	})
}

func Exit(v ...any) {
	_ = std.output(0, 2, EXIT, func(b []byte) []byte {
		return fmt.Append(b, v...)
	})
	panic(fmt.Sprint(v...))
}

func Output(calldepth int, level Level, s string) error {
	calldepth++ // +1 for this frame.
	return std.output(0, calldepth, level, func(b []byte) []byte {
		return append(b, s...)
	})
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
