package hutl

import (
	"sort"
	"strings"
)

// Equal 比较两个相同类型的值，如果它们相等则返回 true，否则返回 false。
func Equal[T comparable](value1, value2 *T) bool {
	if value1 == nil && value2 == nil {
		return true
	}
	if value1 == nil || value2 == nil {
		return false
	}

	if *value1 == *value2 {
		return true
	}
	return false
}

// Slice 对一个切片中的每个元素进行操作，并返回一个新的切片。
func Slice[T any, E any](list []T, f func(i int, v T) E) []E {
	result := make([]E, len(list))
	for i, v := range list {
		result[i] = f(i, v)
	}
	return result
}

// Filter 对一个切片中的每个元素进行操作，并返回一个新的切片，其中只包含满足条件的元素。
func Filter[T any](list []T, f func(T) bool) []T {
	result := make([]T, 0, len(list))
	for _, v := range list {
		if f(v) {
			result = append(result, v)
		}
	}
	return result
}

// Map 对一个 map 中的每个元素进行操作，并返回一个新的 map。
func Map[K comparable, V any, E any](m map[K]V, f func(K, V) E) map[K]E {
	result := make(map[K]E)
	for k, v := range m {
		result[k] = f(k, v)
	}
	return result
}

// SliceToMap 将一个切片转换为一个 map。
func SliceToMap[K comparable, V any, E any](list []E, f func(int, E) (K, V)) map[K]V {
	result := make(map[K]V)
	for i, v := range list {
		_k, _v := f(i, v)
		result[_k] = _v
	}
	return result
}

// Keys 将一个 map的Key 转换为一个切片。
func Keys[K comparable, V any](m map[K]V) []K {
	result := make([]K, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	return result
}

// Values 将一个 map的Value 转换为一个切片。
func Values[K comparable, V any](m map[K]V) []V {
	result := make([]V, 0, len(m))
	for _, v := range m {
		result = append(result, v)
	}
	return result
}

// Contains 判断一个切片是否包含某个元素。
func Contains[T comparable](list []T, value T) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}

// IndexOf 返回一个切片中某个元素的索引。
func IndexOf[T comparable](list []T, value T) int {
	for i, v := range list {
		if v == value {
			return i
		}
	}
	return -1
}

// FirstWhere 返回一个切片中第一个满足条件的元素的索引。
func FirstWhere[T any](list []T, f func(T) bool) int {
	for i, v := range list {
		if f(v) {
			return i
		}
	}
	return -1
}

// LastIndexOf 返回一个切片中最后一个满足条件的元素的索引。
func LastIndexOf[T comparable](list []T, value T) int {
	for i := len(list) - 1; i >= 0; i-- {
		if list[i] == value {
			return i
		}
	}
	return -1
}

// Count 返回一个切片中满足条件的元素的个数。
func Count[T any](list []T, f func(T) bool) int {
	count := 0
	for _, v := range list {
		if f(v) {
			count++
		}
	}
	return count
}

// ForEach 遍历一个切片，并对每个元素进行操作。
func ForEach[T any](list []T, f func(T) error) error {
	for _, v := range list {
		if err := f(v); err != nil {
			return err
		}
	}
	return nil
}

// ToPointer 转为指针类型
func ToPointer[T any](value T) *T {
	return &value
}

// MapToSlice 将一个 map 转换为一个切片。
func MapToSlice[K comparable, V any, E any](m map[K]V, f func(K, V) E) []E {
	result := make([]E, 0, len(m))
	for k, v := range m {
		result = append(result, f(k, v))
	}
	return result
}

// ToAnyList 将一个切片转换为一个 any 切片。
func ToAnyList[T any](list []T) []any {
	result := make([]any, len(list))
	for i, v := range list {
		result[i] = v
	}
	return result
}

func UrlJoin(base string, paths ...string) string {
	base = strings.TrimRight(base, "/")
	for _, path := range paths {
		base += "/" + strings.Trim(path, "/")
	}
	return base
}

// Sort 对一个切片进行排序。
func Sort[T any](list []T, less func(a, b T) bool) {
	sort.Slice(list, func(i, j int) bool {
		return less(list[i], list[j])
	})
}

// Unique 对一个切片中的每个元素进行去重操作，并返回一个新的切片。
func Unique[T any, E comparable](list []T, f func(T) E) []E {
	seen := make(map[E]struct{}, len(list))
	result := make([]E, 0, len(list))

	for _, i := range list {
		v := f(i)
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			result = append(result, v)
		}
	}
	return result
}

// Group 根据一个切片中的元素的某个属性，将元素分组。
func Group[T any, K comparable](list []T, key func(T) K) map[K][]T {
	m := make(map[K][]T)
	for _, item := range list {
		k := key(item)
		m[k] = append(m[k], item)
	}
	return m
}

// Diff 切片差集
func Diff[E any, T comparable](src []E, dsc []E, get func(v E) T) []E {
	// 创建一个map来存储dsc中元素的键值
	dscMap := make(map[T]struct{})
	for _, v := range dsc {
		dscMap[get(v)] = struct{}{}
	}

	// 创建一个切片来存储结果
	var result []E
	for _, v := range src {
		key := get(v)
		if _, exists := dscMap[key]; !exists {
			result = append(result, v)
		}
	}
	return result
}

// Batch 对一个切片进行分批处理。
func Batch[E any](list []E, limit int, fun func(list []E) error) error {
	for j := 0; j < len(list); j += limit {
		end := min(j+limit, len(list))

		if err := fun(list[j:end]); err != nil {
			return err
		}
	}
	return nil
}

// The 找到切片中之最的元素
func The[T any](list []T, fun func(a, b T) bool) T {
	var minItem T
	if len(list) == 0 {
		return minItem
	}
	minItem = list[0]
	for i := 1; i < len(list); i++ {
		if fun(list[i], minItem) {
			minItem = list[i]
		}
	}
	return minItem
}

// Intersect 获得两个切片的交集
func Intersect[T comparable](list1, list2 []T) []T {
	result := make([]T, 0, max(len(list1), len(list2)))
	for _, v := range list1 {
		if Contains(list2, v) {
			result = append(result, v)
		}
	}
	return result
}

// Union 获得两个切片的并集
func Union[T comparable](list1, list2 []T) []T {
	result := make([]T, 0, min(len(list1), len(list2)))
	for _, v := range list1 {
		if !Contains(result, v) {
			result = append(result, v)
		}
	}
	for _, v := range list2 {
		if !Contains(result, v) {
			result = append(result, v)
		}
	}
	return result
}

// PadRight 右对齐字符串
func PadRight(str, pad string, width int) string {
	for len(str) < width {
		str += pad
	}
	return str
}

// PadLeft 左对齐字符串
func PadLeft(str, pad string, width int) string {
	for len(str) < width {
		str = pad + str
	}
	return str
}
