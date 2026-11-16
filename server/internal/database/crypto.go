package database

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"

	"kkomi-timetable/pkg/pathutil"
)

// loadOrCreateMasterKey는 data/master.key에서 AES-256 대칭키를 로드하거나 최초 실행 시 안전하게 생성합니다.
func loadOrCreateMasterKey() ([]byte, error) {
	dataDir, err := pathutil.DataDir()
	if err != nil {
		return nil, err
	}
	keyPath := pathutil.SafeJoin(dataDir, "master.key")
	if raw, readErr := os.ReadFile(keyPath); readErr == nil && len(raw) == 32 {
		return raw, nil
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("마스터 암호화 키 생성 실패: %w", err)
	}
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		return nil, fmt.Errorf("마스터 암호화 키 저장 실패: %w", err)
	}
	return key, nil
}

// EncryptAESGCM은 평문 바이트를 AES-256-GCM으로 암호화합니다. (암호화 자동 백업 등에 사용)
func EncryptAESGCM(key, plaintext []byte) ([]byte, error) {
	sum := sha256.Sum256(key)
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// DecryptAESGCM은 AES-256-GCM 암호문을 복호화합니다.
func DecryptAESGCM(key, ciphertext []byte) ([]byte, error) {
	sum := sha256.Sum256(key)
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(ciphertext) < ns {
		return nil, errors.New("암호화 데이터가 손상되었거나 길이가 짧습니다")
	}
	return gcm.Open(nil, ciphertext[:ns], ciphertext[ns:], nil)
}
