package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SessionManager는 마스터 키 기반 HMAC-SHA256 서명 세션 토큰의 발급·검증·즉시 폐기를 관리합니다.
type SessionManager struct {
	secret          []byte
	mu              sync.RWMutex
	lastIssuedNano  int64
	revoked         map[string]int64
	minIssued       map[string]int64
	globalMinIssued int64
}

// NewSessionManager는 DB 마스터 키에서 파생된 서명 키로 세션 관리자를 초기화합니다.
func NewSessionManager(masterKey []byte) *SessionManager {
	h := sha256.New()
	h.Write(masterKey)
	h.Write([]byte(":kkomi-session-v1"))
	return &SessionManager{
		secret:    h.Sum(nil),
		revoked:   make(map[string]int64),
		minIssued: make(map[string]int64),
	}
}

// IssueToken은 지정된 교사 ID에 대한 서명된 세션 토큰을 발급합니다.
func (m *SessionManager) IssueToken(teacherID string, ttl time.Duration) (string, error) {
	tid := strings.TrimSpace(teacherID)
	if tid == "" {
		return "", errors.New("토큰 발급 대상 교사 ID가 필요합니다")
	}
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}

	var rnd [8]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return "", fmt.Errorf("세션 난수 생성 실패: %w", err)
	}

	now := time.Now()
	expUnix := now.Add(ttl).Unix()

	m.mu.Lock()
	issuedNano := now.UnixNano()
	if issuedNano <= m.lastIssuedNano {
		issuedNano = m.lastIssuedNano + 1
	}
	if minT, ok := m.minIssued[tid]; ok && issuedNano <= minT {
		issuedNano = minT + 1
	}
	if issuedNano <= m.globalMinIssued {
		issuedNano = m.globalMinIssued + 1
	}
	m.lastIssuedNano = issuedNano
	m.mu.Unlock()

	tidEnc := base64.RawURLEncoding.EncodeToString([]byte(tid))
	payload := fmt.Sprintf("%s.%d.%d.%s", tidEnc, issuedNano, expUnix, hex.EncodeToString(rnd[:]))
	sig := m.sign(payload)
	return payload + "." + sig, nil
}

// VerifyToken은 세션 토큰의 서명·만료 시각·폐기 여부를 검증하고 교사 ID를 반환합니다.
func (m *SessionManager) VerifyToken(token string) (string, error) {
	raw := strings.TrimSpace(token)
	parts := strings.Split(raw, ".")
	if len(parts) != 5 {
		return "", errors.New("올바르지 않은 인증 토큰 형식입니다")
	}
	payload := strings.Join(parts[:4], ".")
	expectedSig := m.sign(payload)
	if !hmac.Equal([]byte(parts[4]), []byte(expectedSig)) {
		return "", errors.New("인증 토큰 서명이 유효하지 않습니다")
	}

	tidBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(tidBytes) == 0 {
		return "", errors.New("인증 토큰의 교사 정보가 손상되었습니다")
	}
	tid := string(tidBytes)

	issuedNano, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", errors.New("인증 토큰 발급 시각이 올바르지 않습니다")
	}
	expUnix, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return "", errors.New("인증 토큰 만료 시각이 올바르지 않습니다")
	}
	if time.Now().Unix() > expUnix {
		return "", errors.New("로그인 세션이 만료되었습니다. 다시 로그인해 주세요")
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	if exp, ok := m.revoked[raw]; ok && exp > 0 {
		return "", errors.New("로그아웃되어 만료된 세션입니다")
	}
	if issuedNano <= m.globalMinIssued {
		return "", errors.New("시스템 초기화 또는 복원으로 세션이 만료되었습니다")
	}
	if minT, ok := m.minIssued[tid]; ok && issuedNano <= minT {
		return "", errors.New("비밀번호 변경 또는 재설정으로 기존 세션이 만료되었습니다")
	}
	return tid, nil
}

// RevokeToken은 단일 세션 토큰을 즉시 폐기합니다.
func (m *SessionManager) RevokeToken(token string) {
	raw := strings.TrimSpace(token)
	if raw == "" {
		return
	}
	parts := strings.Split(raw, ".")
	expUnix := time.Now().Add(24 * time.Hour).Unix()
	if len(parts) == 5 {
		if parsed, err := strconv.ParseInt(parts[2], 10, 64); err == nil {
			expUnix = parsed
		}
	}
	nowUnix := time.Now().Unix()
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, v := range m.revoked {
		if v < nowUnix {
			delete(m.revoked, k)
		}
	}
	m.revoked[raw] = expUnix
}

// RevokeTeacher는 특정 교사의 기존 모든 세션을 즉시 무효화합니다.
func (m *SessionManager) RevokeTeacher(teacherID string) {
	tid := strings.TrimSpace(teacherID)
	if tid == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := time.Now().UnixNano()
	if cutoff <= m.lastIssuedNano {
		cutoff = m.lastIssuedNano
	}
	if prev, ok := m.minIssued[tid]; ok && cutoff <= prev {
		cutoff = prev + 1
	}
	m.minIssued[tid] = cutoff
}

// RevokeAll은 전체 교사의 모든 기존 세션을 즉시 무효화합니다. (시스템 초기화/복원 시 호출)
func (m *SessionManager) RevokeAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := time.Now().UnixNano()
	if cutoff <= m.lastIssuedNano {
		cutoff = m.lastIssuedNano
	}
	if cutoff <= m.globalMinIssued {
		cutoff = m.globalMinIssued + 1
	}
	m.globalMinIssued = cutoff
	m.revoked = make(map[string]int64)
}

func (m *SessionManager) sign(payload string) string {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}
