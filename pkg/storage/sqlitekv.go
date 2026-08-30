// 本文件实现 sqlitekv 后端：基于 SQLite 的键值存储（纯 Go 驱动 modernc.org/sqlite）。
package storage

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动
)

// SQLiteKV 是基于 SQLite 的 Backend 实现。
// 表结构：kv(key TEXT PRIMARY KEY, version INTEGER, data BLOB)。
type SQLiteKV struct {
	db *sql.DB
}

// NewSQLiteKV 打开（或创建）SQLite 数据库并初始化表。
func NewSQLiteKV(path string) (*SQLiteKV, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS kv (
			key     TEXT PRIMARY KEY,
			version INTEGER NOT NULL,
			data    BLOB NOT NULL
		);
	`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("storage: init sqlite table: %w", err)
	}
	return &SQLiteKV{db: db}, nil
}

// Close 关闭底层数据库连接。
func (s *SQLiteKV) Close() error {
	return s.db.Close()
}

// Get 实现 Backend。
func (s *SQLiteKV) Get(ctx context.Context, key string) ([]byte, uint64, error) {
	var data []byte
	var version uint64
	err := s.db.QueryRowContext(ctx, `SELECT version, data FROM kv WHERE key = ?`, key).
		Scan(&version, &data)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, 0, &ErrKeyNotFound{Key: key}
		}
		return nil, 0, err
	}
	return data, version, nil
}

// Put 实现 Backend（CAS 语义：通过 UPDATE 影响行数判断版本冲突）。
func (s *SQLiteKV) Put(ctx context.Context, key string, data []byte, expectedVersion uint64) (uint64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	// 查询当前版本
	var currentVersion uint64
	err = tx.QueryRowContext(ctx, `SELECT version FROM kv WHERE key = ?`, key).Scan(&currentVersion)
	switch {
	case err == sql.ErrNoRows:
		// key 不存在：仅允许新建
		if expectedVersion != 0 {
			return 0, &ErrCASMismatch{Key: key, ExpectedVersion: expectedVersion, ActualVersion: 0}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO kv(key, version, data) VALUES(?, 1, ?)`, key, data); err != nil {
			return 0, err
		}
	case err != nil:
		return 0, err
	default:
		// 版本冲突检查
		if currentVersion != expectedVersion {
			return 0, &ErrCASMismatch{Key: key, ExpectedVersion: expectedVersion, ActualVersion: currentVersion}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE kv SET version = ?, data = ? WHERE key = ?`,
			expectedVersion+1, data, key); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return expectedVersion + 1, nil
}

// Delete 实现 Backend。
func (s *SQLiteKV) Delete(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM kv WHERE key = ?`, key)
	return err
}

// List 实现 Backend。
func (s *SQLiteKV) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	var rows *sql.Rows
	var err error
	if prefix == "" {
		rows, err = s.db.QueryContext(ctx, `SELECT key FROM kv ORDER BY key`)
	} else {
		rows, err = s.db.QueryContext(ctx, `SELECT key FROM kv WHERE key LIKE ? ORDER BY key`, prefix+"%")
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}
