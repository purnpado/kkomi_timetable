package database

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"kkomi-timetable/pkg/pathutil"
	"kkomi-timetable/pkg/protocol"
)

// Store는 무설치 포터블 MariaDB(TDE) 연결 및 AES-256 암호화 백업을 통합 관리합니다.
type Store struct {
	mu          sync.RWMutex
	db          *sql.DB
	dbName      string
	dropOnClose bool
	ownsDaemon  bool
	modeLabel   string
	mariaMgr    *PortableMariaDBManager
	masterKey   []byte
}

// Open은 포터블 MariaDB(127.0.0.1)를 기동하고 도메인 스키마에 연결합니다.
func Open() (*Store, error) {
	masterKey, err := loadOrCreateMasterKey()
	if err != nil {
		return nil, err
	}

	var mgr *PortableMariaDBManager
	dbName := "kkomi_timetable"
	dropOnClose := false
	ownsDaemon := false

	if isRunningTests() {
		mgr, err = acquireTestDaemon()
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256([]byte(pathutil.BaseDir()))
		dbName = "kkomi_test_" + hex.EncodeToString(sum[:5])
		dropOnClose = true
	} else {
		mgr, err = NewPortableMariaDBManager(0)
		if err != nil {
			return nil, err
		}
		if _, startErr := mgr.Start(); startErr != nil {
			return nil, fmt.Errorf("포터블 MariaDB 기동 실패: %w", startErr)
		}
		ownsDaemon = !mgr.reused
	}

	cleanupOnFail := func() {
		if ownsDaemon && mgr != nil {
			_ = mgr.Stop()
		}
	}

	bootstrapDB, err := sql.Open("mysql", mgr.DSN(""))
	if err != nil {
		cleanupOnFail()
		return nil, fmt.Errorf("MariaDB 부트스트랩 연결 실패: %w", err)
	}
	createSQL := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci", dbName)
	if _, execErr := bootstrapDB.Exec(createSQL); execErr != nil {
		_ = bootstrapDB.Close()
		cleanupOnFail()
		return nil, fmt.Errorf("데이터베이스(%s) 생성 실패: %w", dbName, execErr)
	}
	_ = bootstrapDB.Close()

	mariaDB, err := sql.Open("mysql", mgr.DSN(dbName))
	if err != nil {
		cleanupOnFail()
		return nil, fmt.Errorf("MariaDB 연결 열기 실패: %w", err)
	}
	if err := mariaDB.Ping(); err != nil {
		_ = mariaDB.Close()
		cleanupOnFail()
		return nil, fmt.Errorf("MariaDB 응답 확인 실패: %w", err)
	}

	mariaDB.SetMaxOpenConns(30)
	mariaDB.SetMaxIdleConns(10)

	label := fmt.Sprintf("포터블 MariaDB (127.0.0.1:%d · AES-256 암호화)", mgr.port)
	if mgr.hasTDE {
		label = fmt.Sprintf("포터블 MariaDB (127.0.0.1:%d · AES-256 TDE)", mgr.port)
	}

	s := &Store{
		db:          mariaDB,
		dbName:      dbName,
		dropOnClose: dropOnClose,
		ownsDaemon:  ownsDaemon,
		modeLabel:   label,
		mariaMgr:    mgr,
		masterKey:   masterKey,
	}
	if err := s.initSchema(); err != nil {
		_ = s.Close()
		return nil, err
	}
	log.Printf("[DB] %s 연결 성공 (스키마: %s)", s.modeLabel, s.dbName)
	return s, nil
}

// ModeLabel은 현재 활성화된 DB 구동 모드를 반환합니다.
func (s *Store) ModeLabel() string {
	return s.modeLabel
}

// Close는 DB 커넥션과 직접 기동한 포터블 MariaDB 자식 프로세스를 안전하게 종료합니다.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		if s.dropOnClose && s.dbName != "kkomi_timetable" {
			_, _ = s.db.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", s.dbName))
		}
		_ = s.db.Close()
		s.db = nil
	}
	if s.ownsDaemon && s.mariaMgr != nil {
		return s.mariaMgr.Stop()
	}
	return nil
}

// initSchema는 InnoDB 기반의 UTF8MB4 도메인 스키마를 생성합니다.
func (s *Store) initSchema() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS kv_records (
			collection VARCHAR(64) NOT NULL,
			id VARCHAR(128) NOT NULL,
			payload LONGTEXT NOT NULL,
			updated_at VARCHAR(32) NOT NULL,
			PRIMARY KEY (collection, id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id VARCHAR(128) PRIMARY KEY,
			event_type VARCHAR(64) NOT NULL,
			actor_id VARCHAR(64) NOT NULL,
			actor_name VARCHAR(64) NOT NULL,
			title VARCHAR(255) NOT NULL,
			message TEXT NOT NULL,
			occurred_at VARCHAR(32) NOT NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("MariaDB 스키마 초기화 실패: %w", err)
		}
	}
	return nil
}

// SaveRecord는 컬렉션과 ID 단위로 도메인 객체를 행 단위 잠금(InnoDB)으로 저장합니다.
func (s *Store) SaveRecord(collection, id string, value interface{}) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	now := time.Now().Format("2006-01-02 15:04:05")

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err = s.db.Exec(
		`INSERT INTO kv_records (collection, id, payload, updated_at) VALUES (?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE payload = VALUES(payload), updated_at = VALUES(updated_at)`,
		collection, id, string(raw), now,
	)
	return err
}

// DeleteRecord는 특정 컬렉션의 레코드를 삭제합니다.
func (s *Store) DeleteRecord(collection, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM kv_records WHERE collection = ? AND id = ?`, collection, id)
	return err
}

// LoadCollection은 특정 컬렉션의 모든 JSON 레코드를 슬라이스로 로드합니다.
func LoadCollection[T any](s *Store, collection string) ([]T, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`SELECT payload FROM kv_records WHERE collection = ? ORDER BY id ASC`, collection)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []T
	for rows.Next() {
		var payload string
		if scanErr := rows.Scan(&payload); scanErr != nil {
			return nil, scanErr
		}
		var item T
		if jsonErr := json.Unmarshal([]byte(payload), &item); jsonErr != nil {
			return nil, jsonErr
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// CountCollection은 특정 컬렉션의 레코드 개수를 반환합니다.
func (s *Store) CountCollection(collection string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var cnt int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM kv_records WHERE collection = ?`, collection).Scan(&cnt)
	return cnt, err
}

// AppendAuditLog는 도메인 이벤트 감사 로그를 DB에 기록합니다.
func (s *Store) AppendAuditLog(evt protocol.DomainEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO audit_logs (id, event_type, actor_id, actor_name, title, message, occurred_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		evt.ID, string(evt.Type), evt.ActorID, evt.ActorName, evt.Title, evt.Message, evt.OccurredAt,
	)
	return err
}

// ListAuditLogs는 최신 감사 로그 목록을 조회합니다.
func (s *Store) ListAuditLogs(limit int) ([]protocol.DomainEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(
		`SELECT id, event_type, actor_id, actor_name, title, message, occurred_at FROM audit_logs ORDER BY occurred_at DESC, id DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []protocol.DomainEvent
	for rows.Next() {
		var e protocol.DomainEvent
		var et string
		if err := rows.Scan(&e.ID, &et, &e.ActorID, &e.ActorName, &e.Title, &e.Message, &e.OccurredAt); err != nil {
			return nil, err
		}
		e.Type = protocol.EventType(et)
		logs = append(logs, e)
	}
	return logs, rows.Err()
}

// ExportEncryptedBackup은 현재 전체 DB 레코드를 AES-256-GCM 암호화 파일(.kbackup)로 백업합니다.
func (s *Store) ExportEncryptedBackup(tag string) (string, error) {
	s.mu.RLock()
	rows, err := s.db.Query(`SELECT collection, id, payload, updated_at FROM kv_records ORDER BY collection, id`)
	if err != nil {
		s.mu.RUnlock()
		return "", err
	}
	type rowItem struct {
		Collection string `json:"collection"`
		ID         string `json:"id"`
		Payload    string `json:"payload"`
		UpdatedAt  string `json:"updatedAt"`
	}
	var snapshot []rowItem
	for rows.Next() {
		var r rowItem
		if scanErr := rows.Scan(&r.Collection, &r.ID, &r.Payload, &r.UpdatedAt); scanErr == nil {
			snapshot = append(snapshot, r)
		}
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
