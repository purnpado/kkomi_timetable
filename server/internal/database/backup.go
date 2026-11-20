package database

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"kkomi-timetable/pkg/pathutil"
)

// BackupRecordRow는 AES-256-GCM 암호화 백업 파일(.kbackup) 내부의 단일 레코드 스냅샷 항목입니다.
type BackupRecordRow struct {
	Collection string `json:"collection"`
	ID         string `json:"id"`
	Payload    string `json:"payload"`
	UpdatedAt  string `json:"updatedAt"`
}

// ExportEncryptedBackup은 현재 전체 DB 레코드를 AES-256-GCM 암호화 파일(.kbackup)로 백업합니다.
func (s *Store) ExportEncryptedBackup(tag string) (string, error) {
	s.mu.RLock()
	rows, err := s.db.Query(`SELECT collection, id, payload, updated_at FROM kv_records ORDER BY collection, id`)
	if err != nil {
		s.mu.RUnlock()
		return "", err
	}
	var snapshot []BackupRecordRow
	for rows.Next() {
		var r BackupRecordRow
		if scanErr := rows.Scan(&r.Collection, &r.ID, &r.Payload, &r.UpdatedAt); scanErr != nil {
			_ = rows.Close()
			s.mu.RUnlock()
			return "", fmt.Errorf("백업 레코드 읽기 실패: %w", scanErr)
		}
		snapshot = append(snapshot, r)
	}
	rowsErr := rows.Err()
	_ = rows.Close()
	s.mu.RUnlock()
	if rowsErr != nil {
		return "", rowsErr
	}

	plain, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	cipherData, err := EncryptAESGCM(s.masterKey, plain)
	if err != nil {
		return "", err
	}

	backupDir, err := pathutil.BackupDir()
	if err != nil {
		return "", err
	}
	if tag == "" {
		tag = "manual"
	}
	filename := fmt.Sprintf("kkomi_backup_%s_%s.kbackup", tag, time.Now().Format("20060102_150405"))
	fullPath := pathutil.SafeJoin(backupDir, filename)
	if err := os.WriteFile(fullPath, cipherData, 0o600); err != nil {
		return "", err
	}
	return fullPath, nil
}

// ListBackups는 backups 폴더의 암호화 백업 목록을 최신순으로 반환합니다.
func (s *Store) ListBackups() ([]string, error) {
	backupDir, err := pathutil.BackupDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".kbackup" {
			names = append(names, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names, nil
}

// RestoreEncryptedBackup은 backups 폴더 내 암호화 백업(.kbackup)을 검증한 뒤
// 복원 직전 안전 백업을 생성하고 단일 DB 트랜잭션으로 전체 데이터를 원복합니다.
func (s *Store) RestoreEncryptedBackup(fileName string) (preBackupPath string, restoredCount int, err error) {
	cleanName := strings.TrimSpace(fileName)
	if cleanName == "" || filepath.Base(cleanName) != cleanName || filepath.Ext(cleanName) != ".kbackup" {
		return "", 0, errors.New("올바르지 않은 백업 파일명입니다 (.kbackup 파일만 복원 가능)")
	}

	backupDir, err := pathutil.BackupDir()
	if err != nil {
		return "", 0, err
	}
	fullPath := pathutil.SafeJoin(backupDir, cleanName)
	cipherData, err := os.ReadFile(fullPath)
	if err != nil {
		return "", 0, fmt.Errorf("백업 파일 읽기 실패: %w", err)
	}

	plain, err := DecryptAESGCM(s.masterKey, cipherData)
	if err != nil {
		return "", 0, fmt.Errorf("백업 파일 복호화 실패 (키 불일치 또는 파일 손상): %w", err)
	}

	var snapshot []BackupRecordRow
	if err := json.Unmarshal(plain, &snapshot); err != nil {
		return "", 0, fmt.Errorf("백업 데이터 형식 파싱 실패: %w", err)
	}
	if len(snapshot) == 0 {
		return "", 0, errors.New("백업 파일이 비어 있어 복원할 수 없습니다")
	}

	teacherCount := 0
	for _, row := range snapshot {
		if row.Collection == "teachers" {
			teacherCount++
		}
	}
	if teacherCount == 0 {
		return "", 0, errors.New("백업 파일에 교사 계정 정보가 없어 복원할 수 없습니다")
	}

	// 복원 직전 현재 상태를 반드시 먼저 백업 (실패 시 복원 중단)
	preBackupPath, err = s.ExportEncryptedBackup("pre_restore")
	if err != nil {
		return "", 0, fmt.Errorf("복원 직전 안전 백업 생성 실패로 복원을 중단합니다: %w", err)
	}

	err = s.WithTx(func(tx *TxStore) error {
		if _, delErr := tx.Exec(`DELETE FROM kv_records`); delErr != nil {
			return fmt.Errorf("기존 레코드 정리 실패: %w", delErr)
		}
		for _, r := range snapshot {
			if r.Collection == "" || r.ID == "" {
				continue
			}
			updatedAt := r.UpdatedAt
			if updatedAt == "" {
				updatedAt = time.Now().Format("2006-01-02 15:04:05")
			}
			if _, insErr := tx.Exec(
				`INSERT INTO kv_records (collection, id, payload, updated_at) VALUES (?, ?, ?, ?)`,
				r.Collection, r.ID, r.Payload, updatedAt,
			); insErr != nil {
				return fmt.Errorf("백업 레코드(%s/%s) 복원 실패: %w", r.Collection, r.ID, insErr)
			}
			restoredCount++
		}
		return nil
	})
	if err != nil {
		return preBackupPath, 0, err
	}
	return preBackupPath, restoredCount, nil
}
