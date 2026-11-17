package htwheel

import "time"

// TaskRecord 是 Record 接口的内置默认实现。
//
// 适合直接用作文档存储（MemoryStore/FileStore）的记录类型，也可作为自定义
// Record 实现的参考模板：只需实现 getter/setter，即可接入任意存储后端。
type TaskRecord struct {
	ID         uint64        `json:"id"`
	Expire     time.Time     `json:"expire"`
	Name       string        `json:"name"`
	Retries    int32         `json:"retries,omitempty"`
	RetryDelay time.Duration `json:"retry_delay,omitempty"`
}

// 编译期断言：*TaskRecord 实现了 Record 接口。
var _ Record = (*TaskRecord)(nil)

func (r *TaskRecord) GetID() uint64                { return r.ID }
func (r *TaskRecord) GetExpire() time.Time         { return r.Expire }
func (r *TaskRecord) GetName() string              { return r.Name }
func (r *TaskRecord) GetRetries() int32            { return r.Retries }
func (r *TaskRecord) GetRetryDelay() time.Duration { return r.RetryDelay }

func (r *TaskRecord) SetID(id uint64)               { r.ID = id }
func (r *TaskRecord) SetExpire(t time.Time)         { r.Expire = t }
func (r *TaskRecord) SetName(name string)           { r.Name = name }
func (r *TaskRecord) SetRetries(n int32)            { r.Retries = n }
func (r *TaskRecord) SetRetryDelay(d time.Duration) { r.RetryDelay = d }
