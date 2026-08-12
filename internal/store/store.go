package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type Project struct {
	Path      string    `json:"path"`
	Label     string    `json:"label,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type Link struct {
	ID           string            `json:"id"`
	SourceKey    string            `json:"sourceKey"`
	TargetKeys   []string          `json:"targetKeys"`
	Fingerprints map[string]string `json:"fingerprints"`
	CreatedAt    time.Time         `json:"createdAt"`
	UpdatedAt    time.Time         `json:"updatedAt"`
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		PRAGMA journal_mode=WAL;
		CREATE TABLE IF NOT EXISTS projects (
			path TEXT PRIMARY KEY,
			label TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS links (
			id TEXT PRIMARY KEY,
			source_key TEXT NOT NULL,
			target_keys_json TEXT NOT NULL,
			fingerprints_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
	`)
	return err
}

func (s *Store) AddProject(project Project) error {
	if project.CreatedAt.IsZero() {
		project.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.Exec(`INSERT INTO projects(path,label,created_at) VALUES(?,?,?)
		ON CONFLICT(path) DO UPDATE SET label=excluded.label`, project.Path, project.Label, project.CreatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *Store) RemoveProject(path string) error {
	_, err := s.db.Exec(`DELETE FROM projects WHERE path=?`, path)
	return err
}

func (s *Store) Projects() ([]Project, error) {
	rows, err := s.db.Query(`SELECT path,label,created_at FROM projects ORDER BY label,path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Project
	for rows.Next() {
		var p Project
		var created string
		if err := rows.Scan(&p.Path, &p.Label, &created); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		result = append(result, p)
	}
	return result, rows.Err()
}

func (s *Store) SaveLink(link Link) error {
	if link.ID == "" {
		return errors.New("link ID is required")
	}
	now := time.Now().UTC()
	if link.CreatedAt.IsZero() {
		link.CreatedAt = now
	}
	link.UpdatedAt = now
	targets, _ := json.Marshal(link.TargetKeys)
	fingerprints, _ := json.Marshal(link.Fingerprints)
	_, err := s.db.Exec(`INSERT INTO links(id,source_key,target_keys_json,fingerprints_json,created_at,updated_at)
		VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET source_key=excluded.source_key,
		target_keys_json=excluded.target_keys_json,fingerprints_json=excluded.fingerprints_json,
		updated_at=excluded.updated_at`, link.ID, link.SourceKey, string(targets), string(fingerprints),
		link.CreatedAt.Format(time.RFC3339Nano), link.UpdatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *Store) Links() ([]Link, error) {
	rows, err := s.db.Query(`SELECT id,source_key,target_keys_json,fingerprints_json,created_at,updated_at FROM links ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Link
	for rows.Next() {
		var link Link
		var targets, fingerprints, created, updated string
		if err := rows.Scan(&link.ID, &link.SourceKey, &targets, &fingerprints, &created, &updated); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(targets), &link.TargetKeys); err != nil {
			return nil, fmt.Errorf("decode link targets: %w", err)
		}
		if err := json.Unmarshal([]byte(fingerprints), &link.Fingerprints); err != nil {
			return nil, fmt.Errorf("decode link fingerprints: %w", err)
		}
		link.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		link.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		result = append(result, link)
	}
	return result, rows.Err()
}

func (s *Store) RemoveLink(id string) error {
	_, err := s.db.Exec(`DELETE FROM links WHERE id=?`, id)
	return err
}
