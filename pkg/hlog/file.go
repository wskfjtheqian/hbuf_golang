package hlog

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/htime"
)

// MaxSize 单个日志文件的最大字节数, 超过后自动轮转。
// 请在 Logger 启动前设置, 运行期修改需自行保证同步。
var MaxSize uint64 = 1024 * 1024 * 1800

type fileWriter struct {
	file   *os.File
	out    *bufio.Writer
	nbytes uint64
	level  Level
	dir    string
	call   func(v1 Level, v2 Level) bool
}

func (f *fileWriter) Compare(call func(v1 Level, v2 Level) bool) {
	f.call = call
}

func newFileWriter(dir string, level Level) *fileWriter {
	ret := &fileWriter{
		level: level,
		dir:   dir,
		call:  func(v1 Level, v2 Level) bool { return v1 <= v2 },
	}
	_ = ret.rotateFile(htime.NowTime())
	return ret
}

func (f *fileWriter) Flush() error {
	if f.out == nil {
		return nil
	}
	return f.out.Flush()
}

func (f *fileWriter) Sync() error {
	if f.file == nil {
		return nil
	}
	return f.file.Sync()
}

// Write 中 call 谓词语义: 返回 true 表示该级别应写入此文件(见 Logger.setSimple)。
func (f *fileWriter) Write(level Level, p []byte) (n int, err error) {
	if f.out == nil {
		return 0, errors.New("hlog: fileWriter not initialized (create failed)")
	}
	if !f.call(level, f.level) {
		return 0, nil
	}
	p = stripAnsi(p)
	if f.nbytes+uint64(len(p)) >= MaxSize {
		if err := f.rotateFile(htime.NowTime()); err != nil {
			return 0, err
		}
	}
	n, err = f.out.Write(p)
	f.nbytes += uint64(n)
	return n, err
}

const bufferSize = 256 * 1024

// rotateFile 先刷盘并关闭旧文件, 再创建新文件, 避免创建失败或同名覆盖时丢失数据。
func (f *fileWriter) rotateFile(now time.Time) error {
	if f.file != nil {
		_ = f.Flush()
		_ = f.file.Close()
		f.file = nil
		f.out = nil
	}

	file, _, err := create(f.dir, f.level.String(), now)
	if err != nil {
		return err
	}
	f.file = file
	f.nbytes = 0
	f.out = bufio.NewWriterSize(f.file, bufferSize)
	return nil
}

// create 创建新的日志文件; 同秒轮转时 logName 相同, 使用 O_EXCL + 序号后缀避免覆盖旧文件。
func create(dir, tag string, now time.Time) (*os.File, string, error) {
	name, link := logName(tag, now)
	base := filepath.Join(dir, name)
	target := base
	for i := 1; ; i++ {
		file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			return file, link, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, "", err
		}
		target = fmt.Sprintf("%s.%d", base, i)
	}
}

// the name for the symlink for tag.
func logName(tag string, t time.Time) (name, link string) {
	name = fmt.Sprintf("%s.%s.%s.%s.%04d%02d%02d-%02d%02d%02d.%d.log",
		program,
		host,
		userName,
		tag,
		t.Year(),
		t.Month(),
		t.Day(),
		t.Hour(),
		t.Minute(),
		t.Second(),
		pid)
	return name, program + "." + tag
}

var (
	pid      = os.Getpid()
	program  = filepath.Base(os.Args[0])
	host     = "unknownhost"
	userName = "unknownuser"
)

func init() {
	h, err := os.Hostname()
	if err == nil {
		host = shortHostname(h)
	}

	current, err := user.Current()
	if err == nil {
		userName = current.Username
	}
	// Sanitize userName since it is used to construct file paths.
	userName = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		default:
			return '_'
		}
		return r
	}, userName)
}

func shortHostname(hostname string) string {
	if i := strings.Index(hostname, "."); i >= 0 {
		return hostname[:i]
	}
	return hostname
}

// stripAnsi 剥离 ANSI 颜色转义序列(\x1b[...m), 避免颜色码污染日志文件内容。
func stripAnsi(p []byte) []byte {
	if bytes.IndexByte(p, 0x1b) < 0 {
		return p
	}
	out := p[:0]
	for i := 0; i < len(p); {
		if p[i] == 0x1b {
			j := i + 1
			for j < len(p) && p[j] != 'm' {
				j++
			}
			if j < len(p) {
				i = j + 1
				continue
			}
		}
		out = append(out, p[i])
		i++
	}
	return out
}
