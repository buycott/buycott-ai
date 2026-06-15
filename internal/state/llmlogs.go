package state

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"buycott/internal/model"
)

type LLMLogStore struct {
	db *sql.DB
}

func NewLLMLogStore(db *sql.DB) *LLMLogStore {
	return &LLMLogStore{db: db}
}

func (s *LLMLogStore) Save(log *model.LLMLog) error {
	if log.ID == "" {
		log.ID = uuid.New().String()
	}
	if log.CreatedAt.IsZero() {
		log.CreatedAt = time.Now().UTC()
	}
	msgs, _ := json.Marshal(log.Messages)
	_, err := s.db.Exec(
		`INSERT INTO llm_logs
		 (id, task_id, role, model, call_type, messages, response, input_tokens, output_tokens, duration_ms, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		log.ID, log.TaskID, log.Role, log.Model, log.CallType,
		string(msgs), log.Response,
		log.InputTokens, log.OutputTokens, log.DurationMs,
		log.CreatedAt.UTC().Format(time.RFC3339),
	)
	return err
}

func (s *LLMLogStore) List(taskID, role string, limit int) ([]*model.LLMLog, error) {
	q := `SELECT id, task_id, role, model, call_type, messages, response,
	             input_tokens, output_tokens, duration_ms, created_at
	      FROM llm_logs WHERE 1=1`
	var args []any
	if taskID != "" {
		q += " AND task_id = ?"
		args = append(args, taskID)
	}
	if role != "" {
		q += " AND role = ?"
		args = append(args, role)
	}
	q += " ORDER BY created_at DESC"
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []*model.LLMLog
	for rows.Next() {
		l, err := scanLLMLog(rows)
		if err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

// RoleTokenStats holds aggregated token usage for one role.
type RoleTokenStats struct {
	Role         string  `json:"role"`
	Model        string  `json:"model"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	Calls        int64   `json:"calls"`
	EstCostUSD   float64 `json:"est_cost_usd"`
}

// per-million token prices [input, output]; keyed on the model fragment after the "/" separator.
var modelPrices = map[string][2]float64{
	"claude-opus-4-8":          {5.00, 25.00},
	"claude-opus-4-7":          {5.00, 25.00},
	"claude-opus-4-6":          {5.00, 25.00},
	"claude-sonnet-4-6":        {3.00, 15.00},
	"claude-haiku-4-5":         {1.00, 5.00},
	"claude-haiku-4-5-20251001":{1.00, 5.00},
	"gpt-4o":                   {2.50, 10.00},
	"gpt-4o-mini":              {0.15, 0.60},
	"gpt-4-turbo":              {10.00, 30.00},
	"gemini-1.5-pro":           {1.25, 5.00},
	"gemini-1.5-flash":         {0.075, 0.30},
	"gemini-2.0-flash":         {0.075, 0.30},
}

func estimateCost(modelName string, inputTok, outputTok int64) float64 {
	// modelName is stored as "provider/model-id" — strip provider prefix.
	key := modelName
	if i := len("anthropic/"); len(modelName) > i && (modelName[:i] == "anthropic/" || modelName[:7] == "openai/" || modelName[:7] == "gemini/") {
		if j := len(modelName); j > 0 {
			for k, c := range modelName {
				if c == '/' {
					key = modelName[k+1:]
					break
				}
			}
		}
	}
	p, ok := modelPrices[key]
	if !ok {
		return 0
	}
	return float64(inputTok)/1_000_000*p[0] + float64(outputTok)/1_000_000*p[1]
}

// TokenStats returns per-role aggregated token usage and estimated cost.
func (s *LLMLogStore) TokenStats() ([]RoleTokenStats, error) {
	rows, err := s.db.Query(`
		SELECT role, model,
		       SUM(input_tokens), SUM(output_tokens), COUNT(*)
		FROM llm_logs
		GROUP BY role, model
		ORDER BY role, model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []RoleTokenStats
	for rows.Next() {
		var rs RoleTokenStats
		if err := rows.Scan(&rs.Role, &rs.Model, &rs.InputTokens, &rs.OutputTokens, &rs.Calls); err != nil {
			return nil, err
		}
		rs.EstCostUSD = estimateCost(rs.Model, rs.InputTokens, rs.OutputTokens)
		stats = append(stats, rs)
	}
	return stats, rows.Err()
}

func scanLLMLog(sc dbScanner) (*model.LLMLog, error) {
	var l model.LLMLog
	var msgsJSON, createdAt string
	err := sc.Scan(
		&l.ID, &l.TaskID, &l.Role, &l.Model, &l.CallType,
		&msgsJSON, &l.Response,
		&l.InputTokens, &l.OutputTokens, &l.DurationMs,
		&createdAt,
	)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(msgsJSON), &l.Messages)
	l.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &l, nil
}
