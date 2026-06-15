package state

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"buycott/internal/model"
)

type EventStore struct {
	db *sql.DB
}

func NewEventStore(db *sql.DB) *EventStore {
	return &EventStore{db: db}
}

func (s *EventStore) Append(eventType string, payload map[string]any) error {
	p, _ := json.Marshal(payload)
	id := uuid.New().String()
	_, err := s.db.Exec(
		`INSERT INTO events (id, type, payload, created_at) VALUES (?,?,?,?)`,
		id, eventType, string(p), time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

func (s *EventStore) List(limit int) ([]*model.Event, error) {
	q := `SELECT id, type, payload, created_at FROM events ORDER BY created_at ASC`
	var args []any
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*model.Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *EventStore) Since(after time.Time) ([]*model.Event, error) {
	rows, err := s.db.Query(
		`SELECT id, type, payload, created_at FROM events WHERE created_at > ? ORDER BY created_at ASC`,
		after.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*model.Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func scanEvent(sc dbScanner) (*model.Event, error) {
	var e model.Event
	var payloadJSON, createdAt string

	if err := sc.Scan(&e.ID, &e.Type, &payloadJSON, &createdAt); err != nil {
		return nil, err
	}

	_ = json.Unmarshal([]byte(payloadJSON), &e.Payload)
	e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &e, nil
}
