package utl

import (
	"errors"
	"testing"
)

// 测试 Equal 函数
func TestEqual(t *testing.T) {
	// 测试两个 nil 指针
	t.Run("两个 nil 指针", func(t *testing.T) {
		if !Equal[int](nil, nil) {
			t.Error("两个 nil 指针应该返回 true")
		}
	})

	// 测试一个 nil 指针和一个非 nil 指针
	t.Run("一个 nil 指针和一个非 nil 指针", func(t *testing.T) {
		value := 1
		if Equal[int](nil, &value) {
			t.Error("一个 nil 指针和一个非 nil 指针应该返回 false")
		}
	})

	// 测试两个相同值的指针
	t.Run("两个相同值的指针", func(t *testing.T) {
		value1 := 1
		value2 := 1
		if !Equal[int](&value1, &value2) {
			t.Error("两个相同值的指针应该返回 true")
		}
	})

	// 测试两个不同值的指针
	t.Run("两个不同值的指针", func(t *testing.T) {
		value1 := 1
		value2 := 2
		if Equal[int](&value1, &value2) {
			t.Error("两个不同值的指针应该返回 false")
		}
	})
}

// 测试 Slice 函数
func TestSlice(t *testing.T) {
	// 测试正常情况
	t.Run("正常情况", func(t *testing.T) {
		list := []int{1, 2, 3}
		result := Slice[int, int](list, func(v int) int {
			return v * 2
		})
		expected := []int{2, 4, 6}
		if !equalSlice(result, expected) {
			t.Errorf("期望 %v, 得到 %v", expected, result)
		}
	})

	// 测试空切片
	t.Run("空切片", func(t *testing.T) {
		list := []int{}
		result := Slice[int, int](list, func(v int) int {
			return v * 2
		})
		if len(result) != 0 {
			t.Error("空切片应该返回空切片")
		}
	})
}

// 测试 Filter 函数
func TestFilter(t *testing.T) {
	// 测试正常情况
	t.Run("正常情况", func(t *testing.T) {
		list := []int{1, 2, 3, 4, 5}
		result := Filter[int](list, func(v int) bool {
			return v%2 == 0
		})
		expected := []int{2, 4}
		if !equalSlice(result, expected) {
			t.Errorf("期望 %v, 得到 %v", expected, result)
		}
	})

	// 测试空切片
	t.Run("空切片", func(t *testing.T) {
		list := []int{}
		result := Filter[int](list, func(v int) bool {
			return v%2 == 0
		})
		if len(result) != 0 {
			t.Error("空切片应该返回空切片")
		}
	})
}

// 测试 Map 函数
func TestMap(t *testing.T) {
	// 测试正常情况
	t.Run("正常情况", func(t *testing.T) {
		m := map[string]int{"a": 1, "b": 2}
		result := Map[string, int, int](m, func(k string, v int) int {
			return v * 2
		})
		expected := map[string]int{"a": 2, "b": 4}
		if !equalMap(result, expected) {
			t.Errorf("期望 %v, 得到 %v", expected, result)
		}
	})

	// 测试空 map
	t.Run("空 map", func(t *testing.T) {
		m := map[string]int{}
		result := Map[string, int, int](m, func(k string, v int) int {
			return v * 2
		})
		if len(result) != 0 {
			t.Error("空 map 应该返回空 map")
		}
	})
}

// 测试 SliceToMap 函数
func TestSliceToMap(t *testing.T) {
	// 测试正常情况
	t.Run("正常情况", func(t *testing.T) {
		list := []string{"a", "b", "c"}
		result := SliceToMap[string, int](list, func(i int, v string) int {
			return i + 1
		})
		expected := map[string]int{"a": 1, "b": 2, "c": 3}
		if !equalMap(result, expected) {
			t.Errorf("期望 %v, 得到 %v", expected, result)
		}
	})

	// 测试空切片
	t.Run("空切片", func(t *testing.T) {
		list := []string{}
		result := SliceToMap[string, int](list, func(i int, v string) int {
			return i + 1
		})
		if len(result) != 0 {
			t.Error("空切片应该返回空 map")
		}
	})
}

// 测试 Keys 函数
func TestKeys(t *testing.T) {
	// 测试正常情况
	t.Run("正常情况", func(t *testing.T) {
		m := map[string]int{"a": 1, "b": 2}
		result := Keys[string, int](m)
		expected := []string{"a", "b"}
		if !equalSlice(result, expected) {
			t.Errorf("期望 %v, 得到 %v", expected, result)
		}
	})

	// 测试空 map
	t.Run("空 map", func(t *testing.T) {
		m := map[string]int{}
		result := Keys[string, int](m)
		if len(result) != 0 {
			t.Error("空 map 应该返回空切片")
		}
	})
}

// 测试 Values 函数
func TestValues(t *testing.T) {
	// 测试正常情况
	t.Run("正常情况", func(t *testing.T) {
		m := map[string]int{"a": 1, "b": 2}
		result := Values[string, int](m)
		expected := []int{1, 2}
		if !equalSlice(result, expected) {
			t.Errorf("期望 %v, 得到 %v", expected, result)
		}
	})

	// 测试空 map
	t.Run("空 map", func(t *testing.T) {
		m := map[string]int{}
		result := Values[string, int](m)
		if len(result) != 0 {
			t.Error("空 map 应该返回空切片")
		}
	})
}

// 测试 Contains 函数
func TestContains(t *testing.T) {
	// 测试包含元素
	t.Run("包含元素", func(t *testing.T) {
		list := []int{1, 2, 3}
		if !Contains[int](list, 2) {
			t.Error("切片应该包含元素 2")
		}
	})

	// 测试不包含元素
	t.Run("不包含元素", func(t *testing.T) {
		list := []int{1, 2, 3}
		if Contains[int](list, 4) {
			t.Error("切片不应该包含元素 4")
		}
	})

	// 测试空切片
	t.Run("空切片", func(t *testing.T) {
		list := []int{}
		if Contains[int](list, 1) {
			t.Error("空切片不应该包含任何元素")
		}
	})
}

// 测试 IndexOf 函数
func TestIndexOf(t *testing.T) {
	// 测试元素存在
	t.Run("元素存在", func(t *testing.T) {
		list := []int{1, 2, 3}
		if IndexOf[int](list, 2) != 1 {
			t.Error("元素 2 的索引应该是 1")
		}
	})

	// 测试元素不存在
	t.Run("元素不存在", func(t *testing.T) {
		list := []int{1, 2, 3}
		if IndexOf[int](list, 4) != -1 {
			t.Error("元素 4 不应该存在于切片中")
		}
	})

	// 测试空切片
	t.Run("空切片", func(t *testing.T) {
		list := []int{}
		if IndexOf[int](list, 1) != -1 {
			t.Error("空切片不应该包含任何元素")
		}
	})
}

// 测试 FirstWhere 函数
func TestFirstWhere(t *testing.T) {
	// 测试找到满足条件的元素
	t.Run("找到满足条件的元素", func(t *testing.T) {
		list := []int{1, 2, 3, 4, 5}
		index := FirstWhere[int](list, func(v int) bool {
			return v%2 == 0
		})
		if index != 1 {
			t.Error("第一个满足条件的元素索引应该是 1")
		}
	})

	// 测试没有满足条件的元素
	t.Run("没有满足条件的元素", func(t *testing.T) {
		list := []int{1, 3, 5}
		index := FirstWhere[int](list, func(v int) bool {
			return v%2 == 0
		})
		if index != -1 {
			t.Error("没有满足条件的元素应该返回 -1")
		}
	})

	// 测试空切片
	t.Run("空切片", func(t *testing.T) {
		list := []int{}
		index := FirstWhere[int](list, func(v int) bool {
			return v%2 == 0
		})
		if index != -1 {
			t.Error("空切片应该返回 -1")
		}
	})
}

// 测试 LastIndexOf 函数
func TestLastIndexOf(t *testing.T) {
	// 测试元素存在
	t.Run("元素存在", func(t *testing.T) {
		list := []int{1, 2, 3, 2, 1}
		if LastIndexOf[int](list, 2) != 3 {
			t.Error("元素 2 的最后索引应该是 3")
		}
	})

	// 测试元素不存在
	t.Run("元素不存在", func(t *testing.T) {
		list := []int{1, 2, 3}
		if LastIndexOf[int](list, 4) != -1 {
			t.Error("元素 4 不应该存在于切片中")
		}
	})

	// 测试空切片
	t.Run("空切片", func(t *testing.T) {
		list := []int{}
		if LastIndexOf[int](list, 1) != -1 {
			t.Error("空切片不应该包含任何元素")
		}
	})
}

// 测试 Count 函数
func TestCount(t *testing.T) {
	// 测试满足条件的元素个数
	t.Run("满足条件的元素个数", func(t *testing.T) {
		list := []int{1, 2, 3, 4, 5}
		count := Count[int](list, func(v int) bool {
			return v%2 == 0
		})
		if count != 2 {
			t.Error("满足条件的元素个数应该是 2")
		}
	})

	// 测试没有满足条件的元素
	t.Run("没有满足条件的元素", func(t *testing.T) {
		list := []int{1, 3, 5}
		count := Count[int](list, func(v int) bool {
			return v%2 == 0
		})
		if count != 0 {
			t.Error("没有满足条件的元素应该返回 0")
		}
	})

	// 测试空切片
	t.Run("空切片", func(t *testing.T) {
		list := []int{}
		count := Count[int](list, func(v int) bool {
			return v%2 == 0
		})
		if count != 0 {
			t.Error("空切片应该返回 0")
		}
	})
}

// 测试 ForEach 函数
func TestForEach(t *testing.T) {
	// 测试正常情况
	t.Run("正常情况", func(t *testing.T) {
		list := []int{1, 2, 3}
		sum := 0
		err := ForEach[int](list, func(v int) error {
			sum += v
			return nil
		})
		if err != nil {
			t.Error("不应该返回错误")
		}
		if sum != 6 {
			t.Error("期望 sum 为 6")
		}
	})

	// 测试返回错误
	t.Run("返回错误", func(t *testing.T) {
		list := []int{1, 2, 3}
		err := ForEach[int](list, func(v int) error {
			return errors.New("error")
		})
		if err == nil {
			t.Error("应该返回错误")
		}
	})

	// 测试空切片
	t.Run("空切片", func(t *testing.T) {
		list := []int{}
		err := ForEach[int](list, func(v int) error {
			return nil
		})
		if err != nil {
			t.Error("空切片不应该返回错误")
		}
	})
}

// 测试 ToPointer 函数
func TestToPointer(t *testing.T) {
	// 测试正常情况
	t.Run("正常情况", func(t *testing.T) {
		value := 1
		ptr := ToPointer[int](value)
		if *ptr != value {
			t.Error("指针指向的值应该与原始值相同")
		}
	})
}

// 测试 MapToSlice 函数
func TestMapToSlice(t *testing.T) {
	// 测试正常情况
	t.Run("正常情况", func(t *testing.T) {
		m := map[string]int{"a": 1, "b": 2}
		result := MapToSlice[string, int, int](m, func(k string, v int) int {
			return v * 2
		})
		expected := []int{2, 4}
		if !equalSlice(result, expected) {
			t.Errorf("期望 %v, 得到 %v", expected, result)
		}
	})

	// 测试空 map
	t.Run("空 map", func(t *testing.T) {
		m := map[string]int{}
		result := MapToSlice[string, int, int](m, func(k string, v int) int {
			return v * 2
		})
		if len(result) != 0 {
			t.Error("空 map 应该返回空切片")
		}
	})
}

// 辅助函数：比较两个切片是否相等
func equalSlice[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// 辅助函数：比较两个 map 是否相等
func equalMap[K comparable, V comparable](a, b map[K]V) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
