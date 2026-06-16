package state

import (
	"database/sql"
	"time"

	"buycott/internal/model"
	"github.com/google/uuid"
)

type ReleaseStore struct {
	db *sql.DB
}

func NewReleaseStore(db *sql.DB) *ReleaseStore {
	return &ReleaseStore{db: db}
}

func (s *ReleaseStore) Save(r *model.Release) error {
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	_, err := s.db.Exec(
		`INSERT INTO releases (id, version, notes, path, created_at) VALUES (?,?,?,?,?)
		 ON CONFLICT(version) DO UPDATE SET notes=excluded.notes, path=excluded.path`,
		r.ID, r.Version, r.Notes, r.Path,
		r.CreatedAt.UTC().Format(time.RFC3339),
	)
	return err
}

func (s *ReleaseStore) List() ([]*model.Release, error) {
	rows, err := s.db.Query(
		`SELECT id, version, notes, path, created_at FROM releases ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var releases []*model.Release
	for rows.Next() {
		var r model.Release
		var createdAt string
		if err := rows.Scan(&r.ID, &r.Version, &r.Notes, &r.Path, &createdAt); err != nil {
			return nil, err
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		releases = append(releases, &r)
	}
	return releases, rows.Err()
}

func (s *ReleaseStore) Latest() (*model.Release, error) {
	var r model.Release
	var createdAt string
	err := s.db.QueryRow(
		`SELECT id, version, notes, path, created_at FROM releases ORDER BY created_at DESC LIMIT 1`,
	).Scan(&r.ID, &r.Version, &r.Notes, &r.Path, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &r, nil
}
