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
