package utl

import (
	"testing"
)

// 测试 AesEncryptCBC 和 AesDecryptCBC 函数
func TestAesCBC(t *testing.T) {
	// 测试用例1: 正常加密解密
	key := []byte("0123456789abcdef")
	content := []byte("hello world")
	encrypted, err := AesEncryptCBC(content, key)
	if err != nil {
		t.Errorf("AesEncryptCBC failed: %v", err)
	}
	decrypted, err := AesDecryptCBC(encrypted, key)
	if err != nil {
		t.Errorf("AesDecryptCBC failed: %v", err)
	}
	if string(decrypted) != string(content) {
		t.Errorf("Decrypted content does not match original content")
	}

	// 测试用例2: 密钥长度不足
	shortKey := []byte("shortkey")
	_, err = AesEncryptCBC(content, shortKey)
	if err == nil {
		t.Errorf("AesEncryptCBC should fail with short key")
	}

	// 测试用例3: 空内容加密
	emptyContent := []byte("")
	encrypted, err = AesEncryptCBC(emptyContent, key)
	if err != nil {
		t.Errorf("AesEncryptCBC failed with empty content: %v", err)
	}
	decrypted, err = AesDecryptCBC(encrypted, key)
	if err != nil {
		t.Errorf("AesDecryptCBC failed with empty content: %v", err)
	}
	if string(decrypted) != string(emptyContent) {
		t.Errorf("Decrypted content does not match original empty content")
	}
}

// 测试 AesEncryptECB 和 AesDecryptECB 函数
func TestAesECB(t *testing.T) {
	// 测试用例1: 正常加密解密
	key := []byte("0123456789abcdef")
	content := []byte("hello world")
	encrypted := AesEncryptECB(content, key)
	decrypted := AesDecryptECB(encrypted, key)
	if string(decrypted) != string(content) {
		t.Errorf("Decrypted content does not match original content")
	}

	// 测试用例2: 密钥长度不足
	shortKey := []byte("shortkey")
	encrypted = AesEncryptECB(content, shortKey)
	decrypted = AesDecryptECB(encrypted, shortKey)
	if string(decrypted) != string(content) {
		t.Errorf("Decrypted content does not match original content with short key")
	}

	// 测试用例3: 空内容加密
	emptyContent := []byte("")
	encrypted = AesEncryptECB(emptyContent, key)
	decrypted = AesDecryptECB(encrypted, key)
	if string(decrypted) != string(emptyContent) {
		t.Errorf("Decrypted content does not match original empty content")
	}
}

// 测试 AesEncryptCFB 和 AesDecryptCFB 函数
func TestAesCFB(t *testing.T) {
	// 测试用例1: 正常加密解密
	key := []byte("0123456789abcdef")
	content := []byte("hello world")
	encrypted, err := AesEncryptCFB(content, key)
	if err != nil {
		t.Errorf("AesEncryptCFB failed: %v", err)
	}
	decrypted, err := AesDecryptCFB(encrypted, key)
	if err != nil {
		t.Errorf("AesDecryptCFB failed: %v", err)
	}
	if string(decrypted) != string(content) {
		t.Errorf("Decrypted content does not match original content")
	}

	// 测试用例2: 密钥长度不足
	shortKey := []byte("shortkey")
	_, err = AesEncryptCFB(content, shortKey)
	if err == nil {
		t.Errorf("AesEncryptCFB should fail with short key")
	}

	// 测试用例3: 空内容加密
	emptyContent := []byte("")
	encrypted, err = AesEncryptCFB(emptyContent, key)
	if err != nil {
		t.Errorf("AesEncryptCFB failed with empty content: %v", err)
	}
	decrypted, err = AesDecryptCFB(encrypted, key)
	if err != nil {
		t.Errorf("AesDecryptCFB failed with empty content: %v", err)
	}
	if string(decrypted) != string(emptyContent) {
		t.Errorf("Decrypted content does not match original empty content")
	}

	// 测试用例4: 加密内容长度不足
	shortContent := []byte("short")
	encrypted, err = AesEncryptCFB(shortContent, key)
	if err != nil {
		t.Errorf("AesEncryptCFB failed with short content: %v", err)
	}
	decrypted, err = AesDecryptCFB(encrypted, key)
	if err != nil {
		t.Errorf("AesDecryptCFB failed with short content: %v", err)
	}
	if string(decrypted) != string(shortContent) {
		t.Errorf("Decrypted content does not match original short content")
	}
}

// 测试 PKCS7Padding 和 PKCS7UnPadding 函数
func TestPKCS7Padding(t *testing.T) {
	// 测试用例1: 正常填充
	content := []byte("hello")
	blockSize := 8
	padded := PKCS7Padding(content, blockSize)
	if len(padded)%blockSize != 0 {
		t.Errorf("PKCS7Padding failed: padded content length is not a multiple of block size")
	}
	unpadded := PKCS7UnPadding(padded)
	if string(unpadded) != string(content) {
		t.Errorf("PKCS7UnPadding failed: unpadded content does not match original content")
	}

	// 测试用例2: 空内容填充
	emptyContent := []byte("")
	padded = PKCS7Padding(emptyContent, blockSize)
	if len(padded)%blockSize != 0 {
		t.Errorf("PKCS7Padding failed with empty content: padded content length is not a multiple of block size")
	}
	unpadded = PKCS7UnPadding(padded)
	if string(unpadded) != string(emptyContent) {
		t.Errorf("PKCS7UnPadding failed with empty content: unpadded content does not match original empty content")
	}

	// 测试用例3: 内容长度已经是块大小的倍数
	content = []byte("12345678")
	padded = PKCS7Padding(content, blockSize)
	if len(padded)%blockSize != 0 {
		t.Errorf("PKCS7Padding failed with content length multiple of block size: padded content length is not a multiple of block size")
	}
	unpadded = PKCS7UnPadding(padded)
	if string(unpadded) != string(content) {
		t.Errorf("PKCS7UnPadding failed with content length multiple of block size: unpadded content does not match original content")
	}
}
