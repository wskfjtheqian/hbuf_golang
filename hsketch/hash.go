package hsketch

import (
	"fmt"
	"hash/fnv"
	"unsafe"
)

// Hash  (零内存分配优化版)
func Hash[T comparable](item T) uint64 {
	var data []byte

	// 针对常用类型进行特化处理，避免反射和内存拷贝
	switch v := any(item).(type) {
	case string:
		data = unsafeStringToBytes(v)
	case int64:
		data = unsafeInt64ToBytes(v)
	case uint64:
		data = unsafeUint64ToBytes(v)
	case int:
		// 根据平台位数转换
		if unsafe.Sizeof(v) == 8 {
			data = unsafeInt64ToBytes(int64(v))
		} else {
			data = unsafeInt32ToBytes(int32(v))
		}
	default:
		// 兜底方案：使用 fmt.Sprintf (虽然慢但安全)
		// 生产环境建议只调用特化的 Add/Contains 方法
		data = []byte(fmt.Sprintf("%v", item))
	}

	h1 := fnv.New64a()
	_, _ = h1.Write(data)
	return h1.Sum64()
}

// ===================== Unsafe 工具函数 =====================

// unsafeStringToBytes 将 string 转为 []byte，零拷贝
func unsafeStringToBytes(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

// unsafeInt64ToBytes 将 int64 转为 []byte，零拷贝
func unsafeInt64ToBytes(i int64) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(&i)), 8)
}

// unsafeUint64ToBytes 将 uint64 转为 []byte，零拷贝
func unsafeUint64ToBytes(i uint64) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(&i)), 8)
}

// unsafeInt32ToBytes 将 int32 转为 []byte，零拷贝
func unsafeInt32ToBytes(i int32) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(&i)), 4)
}
