// spool 包在本地独占目录中持久化不可变的 CDC 记录。
package spool

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

const (
	formatVersion   = "CDC1"
	recordSuffix    = ".record"
	pendingPrefix   = ".pending-"
	identityFile    = "identity"
	headerSize      = len(formatVersion) + 16
	encodedOverhead = headerSize + sha256.Size
)

// ErrPublicationUncertain 表示记录已可见，但目录同步失败。重试发布相同记录可确认其持久化状态。
var ErrPublicationUncertain = herror.NewError("spool publication durability is uncertain")

// Store 在关闭前持有缓冲目录的排他所有权。
type Store struct {
	mu   sync.RWMutex
	dir  string
	lock *os.File
}

// Open 取得缓冲目录的排他所有权，并清理上次中断遗留的临时文件。
func Open(dir string) (*Store, error) {
	if dir == "" {
		return nil, herror.NewError("spool directory is empty")
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, herror.Wrap(err)
	}
	if err = ensureDirectory(absolute); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(absolute, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, herror.Wrap(fmt.Errorf("open spool lock: %w", err))
	}
	if err = lockFile(file); err != nil {
		_ = file.Close()
		return nil, herror.Wrap(fmt.Errorf("acquire spool directory %q: %w", absolute, err))
	}
	if err = recoverDirectory(absolute); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &Store{dir: absolute, lock: file}, nil
}

// ensureDirectory 递归创建缺失的目录，并同步其父目录以持久化目录项。
func ensureDirectory(dir string) error {
	info, err := os.Stat(dir)
	if err == nil && info.IsDir() {
		return nil
	}
	if err == nil {
		return herror.NewError("spool path is not a directory")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return herror.Wrap(err)
	}
	parent := filepath.Dir(dir)
	if err = ensureDirectory(parent); err != nil {
		return err
	}
	if err = os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return herror.Wrap(err)
	}
	return syncDirectory(parent)
}

// recoverDirectory 删除尚未发布的临时文件，并同步缓冲目录。
func recoverDirectory(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return herror.Wrap(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), pendingPrefix) {
			if err = os.Remove(filepath.Join(dir, entry.Name())); err != nil {
				return herror.Wrap(fmt.Errorf("remove unpublished spool file: %w", err))
			}
		}
	}
	return syncDirectory(dir)
}

// Bind 为目录写入不可变的身份信息，或校验已存在的身份。
func (s *Store) Bind(metadata []byte) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lock == nil {
		return false, herror.Wrap(fs.ErrClosed)
	}
	if len(metadata) == 0 {
		return false, herror.NewError("spool identity is empty")
	}
	return s.publish(filepath.Join(s.dir, identityFile), 0, metadata)
}

// Metadata 校验并返回目录身份。新目录在 Bind 发布身份前返回空值。
func (s *Store) Metadata() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.lock == nil {
		return nil, herror.Wrap(fs.ErrClosed)
	}
	metadata, err := readEnvelope(filepath.Join(s.dir, identityFile), 0)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return metadata, err
}

// Append 发布不可变的顺序记录。按原序号重试相同内容可确认目录同步结果未知的发布。
func (s *Store) Append(sequence uint64, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lock == nil {
		return herror.Wrap(fs.ErrClosed)
	}
	if sequence == 0 {
		return herror.NewError("spool record sequence must be positive")
	}
	_, err := s.publish(s.recordPath(sequence), sequence, payload)
	return err
}

// CreateTemporaryFile 创建由缓冲所有者管理的暂存文件。调用方须在使用后删除文件。
func (s *Store) CreateTemporaryFile() (*os.File, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.lock == nil {
		return nil, herror.Wrap(fs.ErrClosed)
	}
	file, err := os.CreateTemp(s.dir, pendingPrefix)
	if err != nil {
		return nil, herror.WrapMsg(err, "create CDC spool temporary file")
	}
	return file, nil
}

// AppendFile 以有界内存读取暂存内容，并按同样的封装和持久性规则发布记录。
func (s *Store) AppendFile(sequence uint64, source *os.File, size int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lock == nil {
		return herror.Wrap(fs.ErrClosed)
	}
	if sequence == 0 || size < 0 {
		return herror.NewError("spool record requires a positive sequence and a valid size")
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return herror.WrapMsg(err, "rewind staged spool payload")
	}
	path := s.recordPath(sequence)
	existing, existingSize, existingChecksum, err := openEnvelope(path, sequence)
	if err == nil {
		defer existing.Close()
		checksum := sha256.New()
		if _, err := io.Copy(checksum, existing); err != nil {
			return herror.WrapMsg(err, "read existing spool record")
		}
		if !bytes.Equal(checksum.Sum(nil), existingChecksum[:]) {
			return herror.NewError("spool envelope verification failed: " + filepath.Base(path))
		}
		checksum.Reset()
		written, err := io.Copy(checksum, source)
		if err != nil {
			return herror.WrapMsg(err, "hash staged spool payload")
		}
		if existingSize != size || written != size || !bytes.Equal(checksum.Sum(nil), existingChecksum[:]) {
			return herror.NewError("spool payload conflicts with existing " + filepath.Base(path))
		}
		return s.syncPublication()
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	_, err = s.publishFile(path, func(file *os.File) error {
		// 先预留封装头，再流式写入内容并回填计算出的摘要。
		if _, err := file.Seek(int64(encodedOverhead), io.SeekStart); err != nil {
			return err
		}
		checksum := sha256.New()
		written, err := io.Copy(io.MultiWriter(file, checksum), source)
		if err != nil {
			return err
		}
		if written != size {
			return io.ErrUnexpectedEOF
		}
		header := envelopeHeader(sequence, uint64(size), checksum.Sum(nil))
		_, err = file.WriteAt(header[:], 0)
		return err
	})
	return err
}

// OpenRecord 验证整个已发布记录的摘要，并在有效负载起点返回文件句柄。
func (s *Store) OpenRecord(sequence uint64) (*os.File, int64, error) {
	s.mu.RLock()
	if s.lock == nil {
		s.mu.RUnlock()
		return nil, 0, herror.Wrap(fs.ErrClosed)
	}
	if sequence == 0 {
		s.mu.RUnlock()
		return nil, 0, herror.NewError("spool record sequence must be positive")
	}
	path := s.recordPath(sequence)
	file, size, expected, err := openEnvelope(path, sequence)
	s.mu.RUnlock()
	if err != nil {
		return nil, 0, err
	}
	checksum := sha256.New()
	if _, err := io.Copy(checksum, file); err != nil {
		_ = file.Close()
		return nil, 0, herror.WrapMsg(err, "read spool record payload")
	}
	if !bytes.Equal(checksum.Sum(nil), expected[:]) {
		_ = file.Close()
		return nil, 0, herror.NewError("spool envelope verification failed: " + filepath.Base(path))
	}
	if _, err := file.Seek(int64(encodedOverhead), io.SeekStart); err != nil {
		_ = file.Close()
		return nil, 0, herror.WrapMsg(err, "rewind verified spool record")
	}
	return file, size, nil
}

// publish 在校验已有记录后，原子发布新记录或补做上次未确认的目录同步。
func (s *Store) publish(path string, sequence uint64, payload []byte) (bool, error) {
	existing, err := readEnvelope(path, sequence)
	if err == nil {
		if !bytes.Equal(existing, payload) {
			return false, herror.NewError("spool payload conflicts with existing " + filepath.Base(path))
		}
		return false, s.syncPublication()
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}

	return s.publishFile(path, func(file *os.File) error {
		return writeEnvelope(file, sequence, payload)
	})
}

// publishFile 写入并同步封装，在原子发布后同步目录。
func (s *Store) publishFile(path string, write func(*os.File) error) (bool, error) {
	temporary, err := os.CreateTemp(s.dir, pendingPrefix)
	if err != nil {
		return false, herror.Wrap(fmt.Errorf("create unpublished spool file: %w", err))
	}
	tempPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(tempPath)
	}()

	if err = write(temporary); err != nil {
		return false, herror.Wrap(fmt.Errorf("write spool envelope: %w", err))
	}
	if err = temporary.Sync(); err != nil {
		return false, herror.Wrap(fmt.Errorf("sync spool file: %w", err))
	}
	if err = temporary.Close(); err != nil {
		return false, herror.Wrap(fmt.Errorf("close spool file: %w", err))
	}
	if err = os.Rename(tempPath, path); err != nil {
		return false, herror.Wrap(fmt.Errorf("publish spool file: %w", err))
	}
	if err = s.syncPublication(); err != nil {
		return false, err
	}
	return true, nil
}

// syncPublication 同步发布后的目录，并对结果不确定的同步返回专用错误。
func (s *Store) syncPublication() error {
	if err := syncDirectory(s.dir); err != nil {
		return errors.Join(ErrPublicationUncertain, err)
	}
	return nil
}

// writeEnvelope 将版本、序号、长度、校验和及原始内容写入记录封装。
func writeEnvelope(file io.Writer, sequence uint64, payload []byte) error {
	checksum := sha256.Sum256(payload)
	header := envelopeHeader(sequence, uint64(len(payload)), checksum[:])
	written, err := file.Write(header[:])
	if err != nil {
		return err
	}
	if written != len(header) {
		return io.ErrShortWrite
	}
	written, err = file.Write(payload)
	if err != nil {
		return err
	}
	if written != len(payload) {
		return io.ErrShortWrite
	}
	return nil
}

// envelopeHeader 编码记录版本、序号、负载长度与摘要。
func envelopeHeader(sequence, size uint64, checksum []byte) [encodedOverhead]byte {
	var header [encodedOverhead]byte
	copy(header[:], formatVersion)
	binary.BigEndian.PutUint64(header[len(formatVersion):], sequence)
	binary.BigEndian.PutUint64(header[len(formatVersion)+8:], size)
	copy(header[headerSize:], checksum)
	return header
}

// openEnvelope 校验记录头，并返回位于有效负载起点的文件句柄和摘要。
func openEnvelope(path string, sequence uint64) (*os.File, int64, [sha256.Size]byte, error) {
	var checksum [sha256.Size]byte
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, checksum, err
	}
	if err != nil {
		return nil, 0, checksum, herror.Wrap(err)
	}
	payloadSize := info.Size() - int64(encodedOverhead)
	if !info.Mode().IsRegular() || payloadSize < 0 || payloadSize > math.MaxInt {
		return nil, 0, checksum, herror.NewError("invalid spool envelope: " + filepath.Base(path))
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, checksum, herror.Wrap(err)
	}

	var header [encodedOverhead]byte
	if _, err := io.ReadFull(file, header[:]); err != nil {
		_ = file.Close()
		return nil, 0, checksum, herror.WrapMsg(err, "read spool envelope header")
	}
	encodedSequence := binary.BigEndian.Uint64(header[len(formatVersion):])
	length := binary.BigEndian.Uint64(header[len(formatVersion)+8:])
	if string(header[:len(formatVersion)]) != formatVersion || encodedSequence != sequence || length != uint64(payloadSize) {
		_ = file.Close()
		return nil, 0, checksum, herror.NewError("invalid spool envelope: " + filepath.Base(path))
	}
	copy(checksum[:], header[headerSize:])
	return file, payloadSize, checksum, nil
}

// readEnvelope 读取记录封装，核对格式、序号、长度和校验和后返回原始内容。
func readEnvelope(path string, sequence uint64) ([]byte, error) {
	file, size, expected, err := openEnvelope(path, sequence)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	payload := make([]byte, int(size))
	if _, err := io.ReadFull(file, payload); err != nil {
		return nil, herror.WrapMsg(err, "read spool envelope payload")
	}
	checksum := sha256.Sum256(payload)
	if checksum != expected {
		return nil, herror.NewError("spool envelope verification failed: " + filepath.Base(path))
	}
	return payload, nil
}

// Read 读取并校验指定序号的已发布记录。
func (s *Store) Read(sequence uint64) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.lock == nil {
		return nil, herror.Wrap(fs.ErrClosed)
	}
	if sequence == 0 {
		return nil, herror.NewError("spool record sequence must be positive")
	}
	payload, err := readEnvelope(s.recordPath(sequence), sequence)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, herror.Wrap(err)
	}
	return payload, err
}

// recordPath 根据记录序号生成缓冲文件路径。
func (s *Store) recordPath(sequence uint64) string {
	return filepath.Join(s.dir, fmt.Sprintf("%020d%s", sequence, recordSuffix))
}

// Sequences 按升序返回已发布的记录序号。调用方可据检查点验证连续性，再使用 Read 验证内容。
func (s *Store) Sequences() ([]uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.lock == nil {
		return nil, herror.Wrap(fs.ErrClosed)
	}
	return s.sequences()
}

// sequences 在调用方持锁期间扫描缓冲目录，返回已发布的记录序号。
func (s *Store) sequences() ([]uint64, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, herror.Wrap(err)
	}
	// 记录名具有固定宽度并以零填充，os.ReadDir 的排序与序号顺序一致。
	var sequences []uint64
	for _, entry := range entries {
		name := entry.Name()
		number, ok := strings.CutSuffix(name, recordSuffix)
		if !ok {
			continue
		}
		seq, err := strconv.ParseUint(number, 10, 64)
		if err != nil || seq == 0 || fmt.Sprintf("%020d", seq) != number || !entry.Type().IsRegular() {
			return nil, herror.NewError("invalid published spool entry: " + name)
		}
		sequences = append(sequences, seq)
	}
	return sequences, nil
}

// RemoveThrough 回收序号不大于已确认检查点的记录，并同步目录。
func (s *Store) RemoveThrough(sequence uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lock == nil {
		return herror.Wrap(fs.ErrClosed)
	}
	sequences, err := s.sequences()
	if err != nil {
		return err
	}
	for _, current := range sequences {
		if current > sequence {
			break
		}
		if err = os.Remove(s.recordPath(current)); err != nil {
			return herror.Wrap(fmt.Errorf("remove delivered spool record %d: %w", current, err))
		}
	}
	return syncDirectory(s.dir)
}

// Remove 按序号回收一条已确认记录并同步目录。重复回收可补做上次未确认的目录同步。
func (s *Store) Remove(sequence uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lock == nil {
		return herror.Wrap(fs.ErrClosed)
	}
	if sequence == 0 {
		return herror.NewError("spool record sequence must be positive")
	}
	if err := os.Remove(s.recordPath(sequence)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return herror.Wrap(fmt.Errorf("remove delivered spool record %d: %w", sequence, err))
	}
	return syncDirectory(s.dir)
}

// Close 释放缓冲目录的排他所有权，保留锁文件。
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lock == nil {
		return nil
	}
	file := s.lock
	s.lock = nil
	if err := file.Close(); err != nil {
		return herror.Wrap(fmt.Errorf("close spool lock: %w", err))
	}
	return nil
}

// syncDirectory 同步指定目录的元数据，使文件发布和回收操作持久化。
func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return herror.Wrap(err)
	}
	defer dir.Close()
	if err = dir.Sync(); err != nil {
		return herror.Wrap(fmt.Errorf("sync spool directory %q: %w", path, err))
	}
	return nil
}
