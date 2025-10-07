package hform

import "context"

// CreateToken 创建一个用于验证表单重复提交的token
func CreateToken(ctx context.Context, name string) (string, error) {

	return "", nil
}

// ValidateToken 验证表单提交的token是否有效
func ValidateToken(ctx context.Context, name string, token string) error {

	return nil
}
