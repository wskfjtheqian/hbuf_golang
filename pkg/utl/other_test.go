package utl

import (
	"fmt"
	"testing"
)

func TestEqual(t *testing.T) {
	// 测试两个相同值的指针
	a := 5
	b := 5
	if !Equal(&a, &b) {
		t.Errorf("Expected true, got false")
	}

	// 测试两个不同值的指针
	c := 6
	if Equal(&a, &c) {
		t.Errorf("Expected false, got true")
	}

	// 测试两个 nil 指针
	var nilPtr1 *int = nil
	var nilPtr2 *int = nil
	if !Equal(nilPtr1, nilPtr2) {
		t.Errorf("Expected true, got false")
	}

	// 测试一个 nil 指针和一个非 nil 指针
	if Equal(nilPtr1, &a) {
		t.Errorf("Expected false, got true")
	}
}

func TestSlice(t *testing.T) {
	// 测试 Slice 函数的正常情况
	list := []int{1, 2, 3}
	result := Slice(list, func(v int) string {
		return string(rune(v + '0'))
	})
	expected := []string{"1", "2", "3"}
	for i, v := range result {
		if v != expected[i] {
			t.Errorf("Expected %v, got %v", expected[i], v)
		}
	}
}

func TestFilter(t *testing.T) {
	// 测试 Filter 函数，返回满足条件的元素
	list := []int{1, 2, 3, 4, 5}
	result := Filter(list, func(v int) bool {
		return v%2 == 0
	})
	expected := []int{2, 4}
	if len(result) != len(expected) {
		t.Errorf("Expected %v, got %v", expected, result)
	}
}

func TestMap(t *testing.T) {
	// 测试 Map 函数
	m := map[int]string{1: "one", 2: "two"}
	result := Map(m, func(k int, v string) string {
		return v + "!"
	})
	expected := map[int]string{1: "one!", 2: "two!"}
	for k, v := range expected {
		if result[k] != v {
			t.Errorf("Expected %s, got %s", v, result[k])
		}
	}
}

func TestContains(t *testing.T) {
	// 测试 Contains 函数
	list := []int{1, 2, 3}
	if !Contains(list, 2) {
		t.Errorf("Expected true, got false")
	}
	if Contains(list, 4) {
		t.Errorf("Expected false, got true")
	}
}

func TestIndexOf(t *testing.T) {
	// 测试 IndexOf 函数
	list := []int{1, 2, 3}
	if index := IndexOf(list, 2); index != 1 {
		t.Errorf("Expected index 1, got %d", index)
	}
	if index := IndexOf(list, 4); index != -1 {
		t.Errorf("Expected index -1, got %d", index)
	}
}

func TestFirstWhere(t *testing.T) {
	// 测试 FirstWhere 函数
	list := []int{1, 2, 3}
	if index := FirstWhere(list, func(v int) bool {
		return v > 1
	}); index != 1 {
		t.Errorf("Expected index 1, got %d", index)
	}
	if index := FirstWhere(list, func(v int) bool {
		return v > 3
	}); index != -1 {
		t.Errorf("Expected index -1, got %d", index)
	}
}

func TestLastIndexOf(t *testing.T) {
	// 测试 LastIndexOf 函数
	list := []int{1, 2, 3, 2}
	if index := LastIndexOf(list, 2); index != 3 {
		t.Errorf("Expected index 3, got %d", index)
	}
	if index := LastIndexOf(list, 4); index != -1 {
		t.Errorf("Expected index -1, got %d", index)
	}
}

func TestCount(t *testing.T) {
	// 测试 Count 函数
	list := []int{1, 2, 3, 2}
	count := Count(list, func(v int) bool {
		return v == 2
	})
	if count != 2 {
		t.Errorf("Expected count 2, got %d", count)
	}
}

func TestForEach(t *testing.T) {
	// 测试 ForEach 函数
	list := []int{1, 2, 3}
	err := ForEach(list, func(v int) error {
		if v < 0 {
			return fmt.Errorf("negative value")
		}
		return nil
	})
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestToPointer(t *testing.T) {
	// 测试 ToPointer 函数
	value := 5
	ptr := ToPointer(value)
	if *ptr != value {
		t.Errorf("Expected %d, got %d", value, *ptr)
	}
}
