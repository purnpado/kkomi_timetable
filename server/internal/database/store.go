package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/pkg/pathutil"
)

// Store는 포터블 MariaDB(TDE) 우선 연결 및 내장 AES-256 암호화 폴백 DB를 통합 관리합니다.
type Store struct {
	mu          sync.RWMutex
	db          *sql.DB
	driver      string // "mysql" 또는 "sqlite"
	modeLabel   string
	mariaMgr    *PortableMariaDBManager
	masterKey   []byte
}

// Open은 포터블 MariaDB 기동을 먼저 시도하고, 바이너리가 없는 환경에서는 내장 SQLite(WAL)로 자동 폴백합니다.
func Open() (*Store, error) {
	masterKey, err := loadOrCreateMasterKey()
	if err != nil {
		return nil, err
	}

	s := &Store{
		masterKey: masterKey,
	}

	// 1. 포터블 MariaDB(mariadbd.exe) 기동 시도 (Windows 교무실 공용 서버)
	mgr, mgrErr := NewPortableMariaDBManager(13306)
	if mgrErr == nil && mgr.HasBinary() {
		if dsn, startErr := mgr.Start(); startErr == nil {
			// 데이터베이스 생성 보장 후 연결
			baseDSN := strings.Replace(dsn, "/kkomi_timetable?", "/?", 1)
			if bootstrapDB, bErr := sql.Open("mysql", baseDSN); bErr == nil {
				_, _ = bootstrapDB.Exec("CREATE DATABASE IF NOT EXISTS kkomi_timetable CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci")
				_ = bootstrapDB.Close()
			}
			if mariaDB, openErr := sql.Open("mysql", dsn); openErr == nil {
				if pingErr := mariaDB.Ping(); pingErr == nil {
					mariaDB.SetMaxOpenConns(30)
					mariaDB.SetMaxIdleConns(10)
					s.db = mariaDB
					s.driver = "mysql"
					s.modeLabel = "포터블 MariaDB (AES-256 TDE · Row-level Lock)"
					s.mariaMgr = mgr
					if err := s.initSchema(); err != nil {
						return nil, err
					}
					log.Printf("[DB] %s 연결 성공", s.modeLabel)
					return s, nil
				}
				_ = mariaDB.Close()
			}
			_ = mgr.Stop()
		} else {
			log.Printf("[DB] 포터블 MariaDB 기동 생략 (%v) -> 내장 보안 저장소로 전환", startErr)
		}
	}

	// 2. 내장 SQLite + AES-256 암호화 백업 폴백 (macOS 개발 검증 및 즉시 실행용)
	dataDir, err := pathutil.DataDir()
	if err != nil {
		return nil, err
	}
	dbFile := pathutil.SafeJoin(dataDir, "kkomi_timetable.db")
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", dbFile)
	sqliteDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("내장 SQLite 열기 실패: %w", err)
	}
	sqliteDB.SetMaxOpenConns(1) // SQLite 쓰기 경합 방지
	s.db = sqliteDB
	s.driver = "sqlite"
	s.modeLabel = "내장 SQLite WAL (AES-256 암호화 백업 지원)"
	if err := s.initSchema(); err != nil {
		_ = sqliteDB.Close()
		return nil, err
	}
	log.Printf("[DB] %s 연결 성공 (%s)", s.modeLabel, dbFile)
	return s, nil
}

// ModeLabel은 현재 활성화된 DB 구동 모드를 반환합니다.
func (s *Store) ModeLabel() string {
	return s.modeLabel
}

// Close는 DB 커넥션과 포터블 MariaDB 자식 프로세스를 안전하게 종료합니다.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		_ = s.db.Close()
	}
	if s.mariaMgr != nil {
		return s.mariaMgr.Stop()
	}
	return nil
}

// initSchema는 MariaDB 및 SQLite 모두와 호환되는 도메인 스키마를 생성합니다.
func (s *Store) initSchema() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS kv_records (
			collection VARCHAR(64) NOT NULL,
			id VARCHAR(128) NOT NULL,
			payload TEXT NOT NULL,
			updated_at VARCHAR(32) NOT NULL,
			PRIMARY KEY (collection, id)
		)`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id VARCHAR(128) PRIMARY KEY,
			event_type VARCHAR(64) NOT NULL,
			actor_id VARCHAR(64) NOT NULL,
			actor_name VARCHAR(64) NOT NULL,
			title VARCHAR(255) NOT NULL,
			message TEXT NOT NULL,
			occurred_at VARCHAR(32) NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("스키마 초기화 실패: %w", err)
		}
	}
	return nil
}

// SaveRecord는 컬렉션과 ID 단위로 도메인 객체를 저장(행 단위 갱신)합니다.
func (s *Store) SaveRecord(collection, id string, value interface{}) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	now := time.Now().Format("2006-01-02 15:04:05")

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.driver == "mysql" {
		_, err = s.db.Exec(
			`INSERT INTO kv_records (collection, id, payload, updated_at) VALUES (?, ?, ?, ?)
			 ON DUPLICATE KEY UPDATE payload = VALUES(payload), updated_at = VALUES(updated_at)`,
			collection, id, string(raw), now,
		)
	} else {
		_, err = s.db.Exec(
			`INSERT OR REPLACE INTO kv_records (collection, id, payload, updated_at) VALUES (?, ?, ?, ?)`,
			collection, id, string(raw), now,
		)
	}
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
