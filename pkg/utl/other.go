package utl

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
func Slice[T any, E any](list []T, f func(T) E) []E {
	result := make([]E, len(list))
	for i, v := range list {
		result[i] = f(v)
	}
	return result
}

// Filter 对一个切片中的每个元素进行操作，并返回一个新的切片，其中只包含满足条件的元素。
func Filter[T any](list []T, f func(T) bool) []T {
	result := make([]T, 0)
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
func SliceToMap[T comparable, V any](list []T, f func(int, T) V) map[T]V {
	result := make(map[T]V)
	for i, v := range list {
		result[v] = f(i, v)
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
