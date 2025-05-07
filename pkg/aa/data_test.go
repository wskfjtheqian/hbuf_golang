package hbuf_test

import (
	"testing"
	"unsafe"
)

type S1 struct {
	a int
}

func F1(s *S1) int {
	return s.a
}
func F2(s any) int {
	return s.(*S1).a
}
func F3(s unsafe.Pointer) int {
	return (*S1)(s).a
}
func F4(s unsafe.Pointer) int {

	return (*S1)(s).a
}

func BenchmarkName(b *testing.B) {
	s1 := S1{a: 55}
	b.Run("F1", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for b := 0; b < 10000; b++ {
				F1(&s1)
			}
		}
	})
	b.Run("F2", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for b := 0; b < 10000; b++ {
				F2(&s1)
			}
		}
	})
	b.Run("F3", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for b := 0; b < 10000; b++ {
				F3(unsafe.Pointer(&s1))
			}
		}
	})
}
