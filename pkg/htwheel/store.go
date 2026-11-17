package htwheel

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

/*
========================
   MemoryStore
========================
*/

// MemoryStore 是基于 map 的存储实现，进程退出后数据丢失。
// 适合测试，或作为自定义存储层的参考实现。
type MemoryStore struct {
	mu   sync.RWMutex
	data map[uint64]Record
}

// NewMemoryStore 创建内存存储。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: make(map[uint64]Record)}
}

func (m *MemoryStore) Put(rec Record) error {
	m.mu.Lock()
	m.data[rec.ID] = cloneRecord(rec)
	m.mu.Unlock()
	return nil
}

func (m *MemoryStore) Get(id uint64) (Record, bool, error) {
	m.mu.RLock()
	rec, ok := m.data[id]
	m.mu.RUnlock()
	if !ok {
		return Record{}, false, nil
	}
	return cloneRecord(rec), true, nil
}

func (m *MemoryStore) Delete(id uint64) error {
	m.mu.Lock()
	delete(m.data, id)
	m.mu.Unlock()
	return nil
}

func (m *MemoryStore) Scan(before time.Time, limit int) ([]Record, error) {
	m.mu.RLock()
	recs := make([]Record, 0, len(m.data))
	for _, rec := range m.data {
		if !rec.Expire.After(before) {
			recs = append(recs, cloneRecord(rec))
		}
	}
	m.mu.RUnlock()
	return sortAndLimit(recs, limit), nil
}

/*
========================
   FileStore
========================
*/

// FileStore 把全部记录以 JSON 数组保存在单个文件中，每次变更整体原子重写。
//
// 适用于任务量不大（万级以内）、单写者的场景。并发写同一文件的多个进程不保证一致。
type FileStore struct {
	mu     sync.Mutex
	path   string
	data   map[uint64]Record
	loaded bool
}

// NewFileStore 创建文件存储；文件不存在时会在首次写入时自动创建（含父目录）。
func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

func (f *FileStore) Put(rec Record) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.ensure(); err != nil {
		return err
	}
	f.data[rec.ID] = cloneRecord(rec)
	return f.flush()
}

func (f *FileStore) Get(id uint64) (Record, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.ensure(); err != nil {
		return Record{}, false, err
	}
	rec, ok := f.data[id]
	if !ok {
		return Record{}, false, nil
	}
	return cloneRecord(rec), true, nil
}

func (f *FileStore) Delete(id uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.ensure(); err != nil {
		return err
	}
	if _, ok := f.data[id]; !ok {
		return nil
	}
	delete(f.data, id)
	return f.flush()
}

func (f *FileStore) Scan(before time.Time, limit int) ([]Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.ensure(); err != nil {
		return nil, err
	}
	recs := make([]Record, 0, len(f.data))
	for _, rec := range f.data {
		if !rec.Expire.After(before) {
			recs = append(recs, cloneRecord(rec))
		}
	}
	return sortAndLimit(recs, limit), nil
}

// ensure 惰性加载文件；仅第一次访问时真正读盘。
func (f *FileStore) ensure() error {
	if f.loaded {
		return nil
	}
	b, err := os.ReadFile(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			f.data = make(map[uint64]Record)
			f.loaded = true
			return nil
		}
		return err
	}

	var recs []Record
	if len(bytes.TrimSpace(b)) > 0 {
		if err := json.Unmarshal(b, &recs); err != nil {
			return err
		}
	}
	f.data = make(map[uint64]Record, len(recs))
	for _, rec := range recs {
		f.data[rec.ID] = rec
	}
	f.loaded = true
	return nil
}

// flush 先写临时文件再 rename，保证不会读到写了一半的文件。
func (f *FileStore) flush() error {
	recs := make([]Record, 0, len(f.data))
	for _, rec := range f.data {
		recs = append(recs, rec)
	}
	sortRecords(recs)

	b, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(f.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".htwheel-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()

	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, f.path)
}

func sortRecords(recs []Record) {
	sort.Slice(recs, func(i, j int) bool {
		return recs[i].Expire.Before(recs[j].Expire)
	})
}

func sortAndLimit(recs []Record, limit int) []Record {
	sortRecords(recs)
	if limit > 0 && len(recs) > limit {
		recs = recs[:limit]
	}
	return recs
}
