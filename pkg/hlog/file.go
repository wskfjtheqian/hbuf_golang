package hlog

import (
	"bufio"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"
)

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
	ret := &fileWriter{level: level, dir: dir}
	ret.rotateFile(time.Now())
	return ret
}

func (f *fileWriter) Flush() error {
	return f.out.Flush()
}

func (f *fileWriter) Sync() error {
	return f.file.Sync()
}

func (f *fileWriter) Write(level Level, p []byte) (n int, err error) {
	if f.call(level, f.level) {
		return 0, err
	}
	if f.nbytes+uint64(len(p)) >= MaxSize {
		if err := f.rotateFile(time.Now()); err != nil {
			return 0, err
		}
	}
	n, err = f.out.Write(p)
	f.nbytes += uint64(n)
	return n, err

}

const bufferSize = 256 * 1024

// rotateFile closes the syncBuffer's file and starts a new one.
func (f *fileWriter) rotateFile(now time.Time) error {
	var err error
	file, _, err := create(f.dir, f.level.String(), now)
	if f.file != nil {
		f.Flush()
		f.file.Close()
	}

	f.file = file
	f.nbytes = 0
	if err != nil {
		return err
	}
	f.out = bufio.NewWriterSize(f.file, bufferSize)
	return err
}

func create(dir, tag string, now time.Time) (*os.File, string, error) {
	//onceLogDirs.Do(createLogDirs)
	//if len(logDirs) == 0 {
	//	return nil, "", errors.New("log: no log dirs")
	//}
	//name, link := logName(tag, t)
	//var lastErr error
	//for _, dir := range logDirs {
	//	fname := filepath.Join(dir, name)
	//	f, err := os.Create(fname)
	//	if err == nil {
	//		symlink := filepath.Join(dir, link)
	//		os.Remove(symlink)        // ignore err
	//		os.Symlink(name, symlink) // ignore err
	//		if *logLink != "" {
	//			lsymlink := filepath.Join(*logLink, link)
	//			os.Remove(lsymlink)         // ignore err
	//			os.Symlink(fname, lsymlink) // ignore err
	//		}
	//		return f, fname, nil
	//	}
	//	lastErr = err
	//}

	name, link := logName(tag, now)
	dir = filepath.Join(dir, name)
	file, err := os.Create(dir)
	if err != nil {
		return nil, "", err
	}
	return file, link, nil
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
