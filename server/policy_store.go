package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type policyStore struct {
	db *sql.DB
}

type proposalRecord struct {
	WorkspaceID string `json:"workspaceId"`
	InstanceKey string `json:"instanceKey"`
	ProposalID  string `json:"proposalId"`
	Revision    uint64 `json:"revision"`
	SourceID    string `json:"sourceId"`
	InputJSON   string `json:"inputJson"`
	Approval    string `json:"approvalState"`
	CommandKey  string `json:"commandKey"`
	TaskID      string `json:"taskId,omitempty"`
	ReceiptJSON string `json:"receiptJson,omitempty"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type outboxRecord struct {
	OutboxID         string `json:"outboxId"`
	WorkspaceID      string `json:"workspaceId"`
	InstanceKey      string `json:"instanceKey"`
	ProposalID       string `json:"proposalId"`
	ProposalRevision uint64 `json:"proposalRevision"`
	IdempotencyKey   string `json:"idempotencyKey"`
	PayloadJSON      string `json:"payloadJson"`
	Status           string `json:"status"`
	ReceiptJSON      string `json:"receiptJson,omitempty"`
	LastError        string `json:"lastError,omitempty"`
}

type taskWatchRecord struct {
	WorkspaceID      string   `json:"workspaceId"`
	InstanceKey      string   `json:"instanceKey"`
	TaskID           string   `json:"taskId"`
	Filter           []string `json:"filter"`
	DebounceSeconds  int      `json:"debounceSeconds"`
	MaxFollowupDepth int      `json:"maxFollowupDepth"`
	FollowupDepth    int      `json:"followupDepth"`
	LastTaskRevision string   `json:"lastTaskRevision"`
	LastEventAt      string   `json:"lastEventAt"`
}

type storedTaskClaim struct {
	WorkspaceID     string `json:"workspaceId"`
	InstanceKey     string `json:"instanceKey"`
	TaskID          string `json:"taskId"`
	Generation      int64  `json:"generation"`
	ResourceVersion string `json:"resourceVersion"`
}

type sourceWriteIntent struct {
	WorkspaceID    string `json:"workspaceId"`
	InstanceKey    string `json:"instanceKey"`
	TaskID         string `json:"taskId"`
	RequestID      string `json:"requestId"`
	RequestJSON    string `json:"requestJson"`
	IdempotencyKey string `json:"idempotencyKey"`
	PayloadJSON    string `json:"payloadJson"`
	Status         string `json:"status"`
	ReceiptJSON    string `json:"receiptJson,omitempty"`
	LastError      string `json:"lastError,omitempty"`
}

type sourceWriteStatus struct {
	TaskID      string `json:"taskId"`
	RequestID   string `json:"requestId"`
	Status      string `json:"status"`
	ReceiptJSON string `json:"receiptJson,omitempty"`
	LastError   string `json:"lastError,omitempty"`
}

type outcomeReport struct {
	WorkspaceID string   `json:"workspaceId"`
	InstanceKey string   `json:"instanceKey"`
	TaskID      string   `json:"taskId"`
	Status      string   `json:"status"`
	Verified    bool     `json:"verified"`
	CostUSD     *float64 `json:"costUsd,omitempty"`
	CostState   string   `json:"costState"`
	Provenance  string   `json:"provenance"`
	UpdatedAt   string   `json:"updatedAt"`
}

type roleTemplate struct {
	Key                   string   `json:"key"`
	DisplayName           string   `json:"displayName"`
	DefaultInstructions   string   `json:"defaultInstructions"`
	DefaultBudgetUSD      float64  `json:"defaultBudgetUsd"`
	MaxConcurrentRuns     int      `json:"maxConcurrentRuns"`
	DefaultAgentToolNames []string `json:"defaultAgentToolNames"`
}

func openPolicyStore(dataDir string) (*policyStore, error) {
	if dataDir == "" {
		return nil, errors.New("KANDEV_PLUGIN_DATA_DIR is required for durable coordinator state")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create coordinator data directory: %w", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "coordinator.db"))
	if err != nil {
		return nil, fmt.Errorf("open coordinator database: %w", err)
	}
	db.SetMaxOpenConns(1)
	store := &policyStore{db: db}
	if err := store.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func openPolicyStoreFromEnvironment() (*policyStore, error) {
	return openPolicyStore(os.Getenv("KANDEV_PLUGIN_DATA_DIR"))
}

func (s *policyStore) Close() error {
	return s.db.Close()
}

func (s *policyStore) migrate(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS coordinator_instances (
  workspace_id TEXT NOT NULL,
  instance_key TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (workspace_id, instance_key)
);
CREATE TABLE IF NOT EXISTS role_templates (
  role_key TEXT PRIMARY KEY,
  display_name TEXT NOT NULL,
  default_instructions TEXT NOT NULL,
  default_budget_usd REAL NOT NULL,
  max_concurrent_runs INTEGER NOT NULL,
  default_agent_tools_json TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS coordinator_memory (
  workspace_id TEXT NOT NULL,
  instance_key TEXT NOT NULL,
  memory_key TEXT NOT NULL,
  value TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (workspace_id, instance_key, memory_key)
);
CREATE TABLE IF NOT EXISTS proposals (
  workspace_id TEXT NOT NULL,
  instance_key TEXT NOT NULL,
  proposal_id TEXT NOT NULL,
  revision INTEGER NOT NULL,
  source_id TEXT NOT NULL,
  input_json TEXT NOT NULL,
  approval_state TEXT NOT NULL,
  command_key TEXT NOT NULL,
  task_id TEXT NOT NULL DEFAULT '',
  receipt_json TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (workspace_id, proposal_id, revision),
  UNIQUE (workspace_id, instance_key, source_id, revision)
);
CREATE TABLE IF NOT EXISTS task_watches (
  workspace_id TEXT NOT NULL,
  instance_key TEXT NOT NULL,
  task_id TEXT NOT NULL,
  filter_json TEXT NOT NULL,
  debounce_seconds INTEGER NOT NULL,
  max_followup_depth INTEGER NOT NULL,
  followup_depth INTEGER NOT NULL DEFAULT 0,
  last_task_revision TEXT NOT NULL DEFAULT '',
  last_event_at TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL,
  PRIMARY KEY (workspace_id, instance_key, task_id)
);
CREATE TABLE IF NOT EXISTS event_cursors (
  workspace_id TEXT NOT NULL,
  instance_key TEXT NOT NULL,
  source TEXT NOT NULL,
  cursor TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (workspace_id, instance_key, source)
);
CREATE TABLE IF NOT EXISTS event_receipts (
  workspace_id TEXT NOT NULL,
  instance_key TEXT NOT NULL,
  event_id TEXT NOT NULL,
  task_revision TEXT NOT NULL,
  result TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (workspace_id, instance_key, event_id, task_revision)
);
CREATE TABLE IF NOT EXISTS intent_outbox (
  outbox_id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  instance_key TEXT NOT NULL,
  proposal_id TEXT NOT NULL,
  proposal_revision INTEGER NOT NULL,
  idempotency_key TEXT NOT NULL UNIQUE,
  payload_json TEXT NOT NULL,
  status TEXT NOT NULL,
  receipt_json TEXT NOT NULL DEFAULT '',
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (workspace_id, proposal_id, proposal_revision)
);
CREATE TABLE IF NOT EXISTS routine_occurrences (
  workspace_id TEXT NOT NULL,
  instance_key TEXT NOT NULL,
  automation_id TEXT NOT NULL,
  occurrence_id TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  status TEXT NOT NULL,
  receipt_json TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL,
  PRIMARY KEY (workspace_id, automation_id, occurrence_id),
  UNIQUE (workspace_id, idempotency_key)
);
CREATE TABLE IF NOT EXISTS outcome_reports (
  workspace_id TEXT NOT NULL,
  instance_key TEXT NOT NULL,
  task_id TEXT NOT NULL,
  status TEXT NOT NULL,
  verified INTEGER NOT NULL,
  cost_amount REAL,
  cost_state TEXT NOT NULL,
  provenance TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (workspace_id, instance_key, task_id)
);
CREATE TABLE IF NOT EXISTS coordinator_task_claims (
  workspace_id TEXT NOT NULL,
  instance_key TEXT NOT NULL,
  task_id TEXT NOT NULL,
  generation INTEGER NOT NULL,
  resource_version TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (workspace_id, instance_key, task_id)
);
CREATE TABLE IF NOT EXISTS coordinator_completion_gates (
  workspace_id TEXT NOT NULL,
  instance_key TEXT NOT NULL,
  task_id TEXT NOT NULL,
  revision INTEGER NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (workspace_id, instance_key, task_id)
);
CREATE TABLE IF NOT EXISTS source_write_intents (
  workspace_id TEXT NOT NULL,
  instance_key TEXT NOT NULL,
  task_id TEXT NOT NULL,
  request_id TEXT NOT NULL,
  request_json TEXT NOT NULL DEFAULT '',
  idempotency_key TEXT NOT NULL UNIQUE,
  payload_json TEXT NOT NULL,
  status TEXT NOT NULL,
  receipt_json TEXT NOT NULL DEFAULT '',
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (workspace_id, instance_key, request_id)
);
`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("create coordinator policy schema: %w", err)
	}
	if err := s.ensureColumn(ctx, "proposals", "created_at", `TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE proposals SET created_at = updated_at WHERE created_at = ''`); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "task_watches", "last_event_at", `TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "task_watches", "followup_depth", `INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "task_watches", "last_task_revision", `TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "source_write_intents", "request_json", `TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	return s.seedRoles(ctx)
}

func (s *policyStore) ensureColumn(ctx context.Context, table, column, definition string) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == column {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column+` `+definition); err != nil {
		return fmt.Errorf("add coordinator schema column %s.%s: %w", table, column, err)
	}
	return nil
}

func (s *policyStore) seedRoles(ctx context.Context) error {
	roles := []roleTemplate{
		{Key: "chief-of-staff", DisplayName: "Chief of staff", DefaultInstructions: "Coordinate work, keep decisions visible, and report blockers with evidence.", DefaultBudgetUSD: 10, MaxConcurrentRuns: 2, DefaultAgentToolNames: []string{createTaskTool, rememberTool, recallTool}},
		{Key: "code-reviewer", DisplayName: "Code reviewer", DefaultInstructions: "Review changes, check relevant tests, and report only verified findings.", DefaultBudgetUSD: 5, MaxConcurrentRuns: 1, DefaultAgentToolNames: []string{createTaskTool, rememberTool, recallTool}},
		{Key: "planner", DisplayName: "Planner", DefaultInstructions: "Break requested outcomes into bounded tasks and preserve their dependencies.", DefaultBudgetUSD: 5, MaxConcurrentRuns: 2, DefaultAgentToolNames: []string{createTaskTool, rememberTool, recallTool}},
		{Key: "delivery-lead", DisplayName: "Delivery lead", DefaultInstructions: "Track delivery risks and follow delegated work through verified outcomes.", DefaultBudgetUSD: 10, MaxConcurrentRuns: 2, DefaultAgentToolNames: []string{createTaskTool, rememberTool, recallTool}},
	}
	for _, role := range roles {
		encoded, err := json.Marshal(role.DefaultAgentToolNames)
		if err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO role_templates
			(role_key, display_name, default_instructions, default_budget_usd, max_concurrent_runs, default_agent_tools_json)
			VALUES (?, ?, ?, ?, ?, ?)`, role.Key, role.DisplayName, role.DefaultInstructions, role.DefaultBudgetUSD, role.MaxConcurrentRuns, string(encoded)); err != nil {
			return fmt.Errorf("seed coordinator role template: %w", err)
		}
	}
	return nil
}

func (s *policyStore) ListInstances(ctx context.Context, workspaceID string) ([]coordinatorInstance, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload_json FROM coordinator_instances WHERE workspace_id = ? ORDER BY instance_key`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	instances := []coordinatorInstance{}
	for rows.Next() {
		var encoded string
		if err := rows.Scan(&encoded); err != nil {
			return nil, err
		}
		var instance coordinatorInstance
		if err := json.Unmarshal([]byte(encoded), &instance); err != nil {
			return nil, fmt.Errorf("decode coordinator instance: %w", err)
		}
		instances = append(instances, instance)
	}
	return instances, rows.Err()
}

func (s *policyStore) ReplaceInstances(ctx context.Context, workspaceID string, instances []coordinatorInstance) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM coordinator_instances WHERE workspace_id = ?`, workspaceID); err != nil {
		return err
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	for _, instance := range instances {
		instance.TaskIDs = append([]string(nil), instance.TaskIDs...)
		encoded, err := json.Marshal(instance)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO coordinator_instances (workspace_id, instance_key, payload_json, updated_at) VALUES (?, ?, ?, ?)`, workspaceID, instance.Key, string(encoded), stamp); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *policyStore) ListRoles(ctx context.Context) ([]roleTemplate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT role_key, display_name, default_instructions, default_budget_usd, max_concurrent_runs, default_agent_tools_json FROM role_templates ORDER BY role_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roles := []roleTemplate{}
	for rows.Next() {
		var role roleTemplate
		var toolsJSON string
		if err := rows.Scan(&role.Key, &role.DisplayName, &role.DefaultInstructions, &role.DefaultBudgetUSD, &role.MaxConcurrentRuns, &toolsJSON); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(toolsJSON), &role.DefaultAgentToolNames); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

func (s *policyStore) CreateProposal(ctx context.Context, workspaceID, instanceKey, proposalID, sourceID, inputJSON string) (proposalRecord, bool, error) {
	if proposalID == "" {
		var err error
		proposalID, err = newRequestID()
		if err != nil {
			return proposalRecord{}, false, err
		}
	}
	if sourceID != "" {
		if existing, err := s.getProposalBySource(ctx, workspaceID, instanceKey, sourceID); err == nil {
			return existing, true, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return proposalRecord{}, false, err
		}
	}
	var latestRevision uint64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision), 0) FROM proposals WHERE workspace_id = ? AND proposal_id = ?`, workspaceID, proposalID).Scan(&latestRevision)
	if err != nil {
		return proposalRecord{}, false, err
	}
	revision := latestRevision + 1
	commandKey, err := newRequestID()
	if err != nil {
		return proposalRecord{}, false, err
	}
	commandKey = "coordinator-proposal-" + commandKey
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `INSERT INTO proposals
		(workspace_id, instance_key, proposal_id, revision, source_id, input_json, approval_state, command_key, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?)`, workspaceID, instanceKey, proposalID, revision, sourceID, inputJSON, commandKey, stamp, stamp)
	if err != nil {
		if sourceID != "" {
			if existing, readErr := s.getProposalBySource(ctx, workspaceID, instanceKey, sourceID); readErr == nil {
				return existing, true, nil
			}
		}
		return proposalRecord{}, false, err
	}
	created, err := s.GetProposal(ctx, workspaceID, proposalID, revision)
	return created, false, err
}

func (s *policyStore) getProposalBySource(ctx context.Context, workspaceID, instanceKey, sourceID string) (proposalRecord, error) {
	return scanProposal(s.db.QueryRowContext(ctx, `SELECT workspace_id, instance_key, proposal_id, revision, source_id, input_json,
		approval_state, command_key, task_id, receipt_json, created_at, updated_at FROM proposals
		WHERE workspace_id = ? AND instance_key = ? AND source_id = ? ORDER BY revision DESC LIMIT 1`, workspaceID, instanceKey, sourceID))
}

func (s *policyStore) GetProposal(ctx context.Context, workspaceID, proposalID string, revision uint64) (proposalRecord, error) {
	return scanProposal(s.db.QueryRowContext(ctx, `SELECT workspace_id, instance_key, proposal_id, revision, source_id, input_json,
		approval_state, command_key, task_id, receipt_json, created_at, updated_at FROM proposals
		WHERE workspace_id = ? AND proposal_id = ? AND revision = ?`, workspaceID, proposalID, revision))
}

func scanProposal(row *sql.Row) (proposalRecord, error) {
	var proposal proposalRecord
	err := row.Scan(&proposal.WorkspaceID, &proposal.InstanceKey, &proposal.ProposalID, &proposal.Revision, &proposal.SourceID,
		&proposal.InputJSON, &proposal.Approval, &proposal.CommandKey, &proposal.TaskID, &proposal.ReceiptJSON, &proposal.CreatedAt, &proposal.UpdatedAt)
	return proposal, err
}

func (s *policyStore) ListProposals(ctx context.Context, workspaceID, instanceKey string, limit int) ([]proposalRecord, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT workspace_id, instance_key, proposal_id, revision, source_id, input_json,
		approval_state, command_key, task_id, receipt_json, created_at, updated_at FROM proposals
		WHERE workspace_id = ? AND instance_key = ? ORDER BY updated_at DESC LIMIT ?`, workspaceID, instanceKey, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	proposals := []proposalRecord{}
	for rows.Next() {
		var proposal proposalRecord
		if err := rows.Scan(&proposal.WorkspaceID, &proposal.InstanceKey, &proposal.ProposalID, &proposal.Revision, &proposal.SourceID,
			&proposal.InputJSON, &proposal.Approval, &proposal.CommandKey, &proposal.TaskID, &proposal.ReceiptJSON, &proposal.CreatedAt, &proposal.UpdatedAt); err != nil {
			return nil, err
		}
		proposals = append(proposals, proposal)
	}
	return proposals, rows.Err()
}

func (s *policyStore) ApproveProposal(ctx context.Context, workspaceID, proposalID string, revision uint64) (proposalRecord, outboxRecord, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return proposalRecord{}, outboxRecord{}, false, err
	}
	defer tx.Rollback()
	proposal, err := scanProposal(tx.QueryRowContext(ctx, `SELECT workspace_id, instance_key, proposal_id, revision, source_id, input_json,
		approval_state, command_key, task_id, receipt_json, created_at, updated_at FROM proposals
		WHERE workspace_id = ? AND proposal_id = ? AND revision = ?`, workspaceID, proposalID, revision))
	if err != nil {
		return proposalRecord{}, outboxRecord{}, false, err
	}
	if proposal.Approval != "pending" {
		outbox, outboxErr := scanOutbox(tx.QueryRowContext(ctx, `SELECT outbox_id, workspace_id, instance_key, proposal_id, proposal_revision,
			idempotency_key, payload_json, status, receipt_json, last_error FROM intent_outbox
			WHERE workspace_id = ? AND proposal_id = ? AND proposal_revision = ?`, workspaceID, proposalID, revision))
		if errors.Is(outboxErr, sql.ErrNoRows) {
			if err := tx.Commit(); err != nil {
				return proposalRecord{}, outboxRecord{}, false, err
			}
			return proposal, outboxRecord{}, true, nil
		}
		if outboxErr != nil {
			return proposalRecord{}, outboxRecord{}, false, outboxErr
		}
		if err := tx.Commit(); err != nil {
			return proposalRecord{}, outboxRecord{}, false, err
		}
		return proposal, outbox, true, nil
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `UPDATE proposals SET approval_state = 'approved', updated_at = ?
		WHERE workspace_id = ? AND proposal_id = ? AND revision = ? AND approval_state = 'pending'`, stamp, workspaceID, proposalID, revision); err != nil {
		return proposalRecord{}, outboxRecord{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO intent_outbox
		(outbox_id, workspace_id, instance_key, proposal_id, proposal_revision, idempotency_key, payload_json, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?)`, proposal.CommandKey, workspaceID, proposal.InstanceKey, proposalID, revision,
		proposal.CommandKey, proposal.InputJSON, stamp, stamp); err != nil {
		return proposalRecord{}, outboxRecord{}, false, err
	}
	proposal.Approval = "approved"
	proposal.UpdatedAt = stamp
	outbox := outboxRecord{
		OutboxID: proposal.CommandKey, WorkspaceID: workspaceID, InstanceKey: proposal.InstanceKey,
		ProposalID: proposalID, ProposalRevision: revision, IdempotencyKey: proposal.CommandKey,
		PayloadJSON: proposal.InputJSON, Status: "pending",
	}
	if err := tx.Commit(); err != nil {
		return proposalRecord{}, outboxRecord{}, false, err
	}
	return proposal, outbox, false, nil
}

func scanOutbox(row *sql.Row) (outboxRecord, error) {
	var item outboxRecord
	err := row.Scan(&item.OutboxID, &item.WorkspaceID, &item.InstanceKey, &item.ProposalID, &item.ProposalRevision,
		&item.IdempotencyKey, &item.PayloadJSON, &item.Status, &item.ReceiptJSON, &item.LastError)
	return item, err
}

func (s *policyStore) RejectProposal(ctx context.Context, workspaceID, proposalID string, revision uint64) (proposalRecord, error) {
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `UPDATE proposals SET approval_state = 'rejected', updated_at = ?
		WHERE workspace_id = ? AND proposal_id = ? AND revision = ? AND approval_state = 'pending'`, stamp, workspaceID, proposalID, revision)
	if err != nil {
		return proposalRecord{}, err
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		proposal, readErr := s.GetProposal(ctx, workspaceID, proposalID, revision)
		if readErr != nil {
			return proposalRecord{}, readErr
		}
		return proposal, nil
	}
	return s.GetProposal(ctx, workspaceID, proposalID, revision)
}

func (s *policyStore) ListRecoverableOutbox(ctx context.Context, workspaceID, instanceKey string) ([]outboxRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT outbox_id, workspace_id, instance_key, proposal_id, proposal_revision,
		idempotency_key, payload_json, status, receipt_json, last_error FROM intent_outbox
		WHERE workspace_id = ? AND instance_key = ? AND status IN ('pending', 'dispatching', 'uncertain', 'held') ORDER BY created_at`, workspaceID, instanceKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []outboxRecord{}
	for rows.Next() {
		var item outboxRecord
		if err := rows.Scan(&item.OutboxID, &item.WorkspaceID, &item.InstanceKey, &item.ProposalID, &item.ProposalRevision,
			&item.IdempotencyKey, &item.PayloadJSON, &item.Status, &item.ReceiptJSON, &item.LastError); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *policyStore) MarkOutbox(ctx context.Context, outboxID, state, lastError, receiptJSON, taskID string) error {
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var proposalID, workspaceID string
	var revision uint64
	if err := tx.QueryRowContext(ctx, `SELECT workspace_id, proposal_id, proposal_revision FROM intent_outbox WHERE outbox_id = ?`, outboxID).Scan(&workspaceID, &proposalID, &revision); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE intent_outbox SET status = ?, last_error = ?, receipt_json = ?, updated_at = ? WHERE outbox_id = ?`, state, lastError, receiptJSON, stamp, outboxID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE proposals SET task_id = ?, receipt_json = ?, updated_at = ? WHERE workspace_id = ? AND proposal_id = ? AND revision = ?`, taskID, receiptJSON, stamp, workspaceID, proposalID, revision); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *policyStore) SetTaskWatch(ctx context.Context, watch taskWatchRecord) error {
	if watch.WorkspaceID == "" || watch.InstanceKey == "" || watch.TaskID == "" || watch.DebounceSeconds < 0 || watch.DebounceSeconds > 86400 || watch.MaxFollowupDepth < 0 || watch.MaxFollowupDepth > 10 || watch.FollowupDepth < 0 || watch.FollowupDepth > 10 {
		return errors.New("invalid task watch")
	}
	filterJSON, err := json.Marshal(watch.Filter)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO task_watches
		(workspace_id, instance_key, task_id, filter_json, debounce_seconds, max_followup_depth, followup_depth, last_task_revision, last_event_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, '', ?)
		ON CONFLICT(workspace_id, instance_key, task_id) DO UPDATE SET
		filter_json = excluded.filter_json, debounce_seconds = excluded.debounce_seconds,
		max_followup_depth = excluded.max_followup_depth, followup_depth = excluded.followup_depth,
		last_task_revision = CASE WHEN excluded.last_task_revision = '' THEN task_watches.last_task_revision ELSE excluded.last_task_revision END,
		updated_at = excluded.updated_at`,
		watch.WorkspaceID, watch.InstanceKey, watch.TaskID, string(filterJSON), watch.DebounceSeconds,
		watch.MaxFollowupDepth, watch.FollowupDepth, watch.LastTaskRevision, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *policyStore) ListTaskWatches(ctx context.Context, workspaceID, instanceKey string) ([]taskWatchRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT workspace_id, instance_key, task_id, filter_json, debounce_seconds, max_followup_depth, followup_depth, last_task_revision, last_event_at
		FROM task_watches WHERE workspace_id = ? AND instance_key = ? ORDER BY task_id`, workspaceID, instanceKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	watches := []taskWatchRecord{}
	for rows.Next() {
		watch, err := scanTaskWatch(rows)
		if err != nil {
			return nil, err
		}
		watches = append(watches, watch)
	}
	return watches, rows.Err()
}

func (s *policyStore) ListTaskWatchesForTask(ctx context.Context, workspaceID, taskID string) ([]taskWatchRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT workspace_id, instance_key, task_id, filter_json, debounce_seconds, max_followup_depth, followup_depth, last_task_revision, last_event_at
		FROM task_watches WHERE workspace_id = ? AND task_id = ? ORDER BY instance_key`, workspaceID, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	watches := []taskWatchRecord{}
	for rows.Next() {
		watch, err := scanTaskWatch(rows)
		if err != nil {
			return nil, err
		}
		watches = append(watches, watch)
	}
	return watches, rows.Err()
}

func scanTaskWatch(rows *sql.Rows) (taskWatchRecord, error) {
	var watch taskWatchRecord
	var filterJSON string
	err := rows.Scan(&watch.WorkspaceID, &watch.InstanceKey, &watch.TaskID, &filterJSON,
		&watch.DebounceSeconds, &watch.MaxFollowupDepth, &watch.FollowupDepth, &watch.LastTaskRevision, &watch.LastEventAt)
	if err != nil {
		return taskWatchRecord{}, err
	}
	err = json.Unmarshal([]byte(filterJSON), &watch.Filter)
	return watch, err
}

func (s *policyStore) ProcessWatchedTaskEvent(ctx context.Context, workspaceID, instanceKey, taskID, eventID, eventType, taskRevision, taskTitle, holdReason string) (proposalRecord, string, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return proposalRecord{}, "", false, err
	}
	defer tx.Rollback()
	var watch taskWatchRecord
	var filterJSON string
	err = tx.QueryRowContext(ctx, `SELECT workspace_id, instance_key, task_id, filter_json, debounce_seconds, max_followup_depth, followup_depth, last_task_revision, last_event_at
		FROM task_watches WHERE workspace_id = ? AND instance_key = ? AND task_id = ?`, workspaceID, instanceKey, taskID).Scan(
		&watch.WorkspaceID, &watch.InstanceKey, &watch.TaskID, &filterJSON, &watch.DebounceSeconds, &watch.MaxFollowupDepth,
		&watch.FollowupDepth, &watch.LastTaskRevision, &watch.LastEventAt)
	if errors.Is(err, sql.ErrNoRows) {
		return proposalRecord{}, "unwatched", false, nil
	}
	if err != nil {
		return proposalRecord{}, "", false, err
	}
	if err := json.Unmarshal([]byte(filterJSON), &watch.Filter); err != nil {
		return proposalRecord{}, "", false, err
	}
	var existingResult string
	err = tx.QueryRowContext(ctx, `SELECT result FROM event_receipts WHERE workspace_id = ? AND instance_key = ? AND event_id = ? AND task_revision = ?`, workspaceID, instanceKey, eventID, taskRevision).Scan(&existingResult)
	if err == nil {
		if err := tx.Commit(); err != nil {
			return proposalRecord{}, "", false, err
		}
		return proposalRecord{}, existingResult, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return proposalRecord{}, "", false, err
	}
	result := "filtered"
	if holdReason != "" {
		result = holdReason
	} else if contains(watch.Filter, eventType) {
		result = "accepted"
		if watch.MaxFollowupDepth == 0 || watch.FollowupDepth >= watch.MaxFollowupDepth {
			result = "followup_limit"
		} else if watch.LastEventAt != "" && watch.DebounceSeconds > 0 {
			lastEvent, parseErr := time.Parse(time.RFC3339Nano, watch.LastEventAt)
			if parseErr == nil && time.Since(lastEvent) < time.Duration(watch.DebounceSeconds)*time.Second {
				result = "debounced"
			}
		}
	}
	var proposal proposalRecord
	if result == "accepted" {
		sourceID := "task-event:" + eventID + ":" + taskRevision
		identity := sha256.Sum256([]byte(workspaceID + "\x00" + instanceKey + "\x00" + sourceID))
		proposalID := "event-" + hex.EncodeToString(identity[:16])
		commandKey := "coordinator-event-" + hex.EncodeToString(identity[:16])
		request := taskRequest{
			InstanceKey: instanceKey, Title: "Review update: " + taskTitle,
			Description: fmt.Sprintf("Task %s emitted %s at revision %s. Read its canonical Host state and decide whether a follow-up is needed.", taskID, eventType, taskRevision),
			Priority:    "normal", FollowupDepth: watch.FollowupDepth + 1,
		}
		payload, err := json.Marshal(request)
		if err != nil {
			return proposalRecord{}, "", false, err
		}
		stamp := time.Now().UTC().Format(time.RFC3339Nano)
		_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO proposals
			(workspace_id, instance_key, proposal_id, revision, source_id, input_json, approval_state, command_key, created_at, updated_at)
			VALUES (?, ?, ?, 1, ?, ?, 'pending', ?, ?, ?)`, workspaceID, instanceKey, proposalID, sourceID, string(payload), commandKey, stamp, stamp)
		if err != nil {
			return proposalRecord{}, "", false, err
		}
		proposal, err = scanProposal(tx.QueryRowContext(ctx, `SELECT workspace_id, instance_key, proposal_id, revision, source_id, input_json,
			approval_state, command_key, task_id, receipt_json, created_at, updated_at FROM proposals
			WHERE workspace_id = ? AND instance_key = ? AND source_id = ? ORDER BY revision DESC LIMIT 1`, workspaceID, instanceKey, sourceID))
		if err != nil {
			return proposalRecord{}, "", false, err
		}
		result = "proposal:" + proposal.ProposalID
		if _, err := tx.ExecContext(ctx, `UPDATE task_watches SET last_event_at = ?, last_task_revision = ?, updated_at = ? WHERE workspace_id = ? AND instance_key = ? AND task_id = ?`, stamp, taskRevision, stamp, workspaceID, instanceKey, taskID); err != nil {
			return proposalRecord{}, "", false, err
		}
	} else if result != "paused" && result != "policy_held" {
		stamp := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := tx.ExecContext(ctx, `UPDATE task_watches SET last_task_revision = ?, updated_at = ? WHERE workspace_id = ? AND instance_key = ? AND task_id = ?`, taskRevision, stamp, workspaceID, instanceKey, taskID); err != nil {
			return proposalRecord{}, "", false, err
		}
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `INSERT INTO event_receipts (workspace_id, instance_key, event_id, task_revision, result, created_at) VALUES (?, ?, ?, ?, ?, ?)`, workspaceID, instanceKey, eventID, taskRevision, result, stamp); err != nil {
		return proposalRecord{}, "", false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO event_cursors (workspace_id, instance_key, source, cursor, updated_at) VALUES (?, ?, 'task_events', ?, ?)
		ON CONFLICT(workspace_id, instance_key, source) DO UPDATE SET cursor = excluded.cursor, updated_at = excluded.updated_at`, workspaceID, instanceKey, eventID, stamp); err != nil {
		return proposalRecord{}, "", false, err
	}
	if err := tx.Commit(); err != nil {
		return proposalRecord{}, "", false, err
	}
	return proposal, result, false, nil
}

func (s *policyStore) UpdateWatchRevision(ctx context.Context, workspaceID, instanceKey, taskID, revision string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE task_watches SET last_task_revision = ?, updated_at = ? WHERE workspace_id = ? AND instance_key = ? AND task_id = ?`,
		revision, time.Now().UTC().Format(time.RFC3339Nano), workspaceID, instanceKey, taskID)
	return err
}

func (s *policyStore) SaveTaskClaim(ctx context.Context, claim storedTaskClaim) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO coordinator_task_claims
		(workspace_id, instance_key, task_id, generation, resource_version, updated_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(workspace_id, instance_key, task_id) DO UPDATE SET generation = excluded.generation,
		resource_version = excluded.resource_version, updated_at = excluded.updated_at`,
		claim.WorkspaceID, claim.InstanceKey, claim.TaskID, claim.Generation, claim.ResourceVersion, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *policyStore) GetTaskClaim(ctx context.Context, workspaceID, instanceKey, taskID string) (storedTaskClaim, error) {
	var claim storedTaskClaim
	err := s.db.QueryRowContext(ctx, `SELECT workspace_id, instance_key, task_id, generation, resource_version
		FROM coordinator_task_claims WHERE workspace_id = ? AND instance_key = ? AND task_id = ?`,
		workspaceID, instanceKey, taskID).Scan(&claim.WorkspaceID, &claim.InstanceKey, &claim.TaskID, &claim.Generation, &claim.ResourceVersion)
	return claim, err
}

func (s *policyStore) DeleteTaskClaim(ctx context.Context, workspaceID, instanceKey, taskID string, generation int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM coordinator_task_claims WHERE workspace_id = ? AND instance_key = ? AND task_id = ? AND generation = ?`,
		workspaceID, instanceKey, taskID, generation)
	return err
}

func (s *policyStore) GetCompletionGateRevision(ctx context.Context, workspaceID, instanceKey, taskID string) (int64, error) {
	var revision int64
	err := s.db.QueryRowContext(ctx, `SELECT revision FROM coordinator_completion_gates WHERE workspace_id = ? AND instance_key = ? AND task_id = ?`,
		workspaceID, instanceKey, taskID).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return revision, err
}

func (s *policyStore) SaveCompletionGateRevision(ctx context.Context, workspaceID, instanceKey, taskID string, revision int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO coordinator_completion_gates (workspace_id, instance_key, task_id, revision, updated_at)
		VALUES (?, ?, ?, ?, ?) ON CONFLICT(workspace_id, instance_key, task_id) DO UPDATE SET revision = excluded.revision, updated_at = excluded.updated_at`,
		workspaceID, instanceKey, taskID, revision, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *policyStore) BeginSourceWrite(ctx context.Context, input sourceWriteIntent) (sourceWriteIntent, bool, error) {
	var current sourceWriteIntent
	err := s.db.QueryRowContext(ctx, `SELECT workspace_id, instance_key, task_id, request_id, request_json, idempotency_key, payload_json, status, receipt_json, last_error
		FROM source_write_intents WHERE workspace_id = ? AND instance_key = ? AND request_id = ?`, input.WorkspaceID, input.InstanceKey, input.RequestID).Scan(
		&current.WorkspaceID, &current.InstanceKey, &current.TaskID, &current.RequestID, &current.RequestJSON, &current.IdempotencyKey,
		&current.PayloadJSON, &current.Status, &current.ReceiptJSON, &current.LastError)
	if err == nil {
		if current.TaskID != input.TaskID || current.IdempotencyKey != input.IdempotencyKey || current.RequestJSON != input.PayloadJSON {
			return sourceWriteIntent{}, false, errors.New("source write request identity was reused with different input")
		}
		return current, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return sourceWriteIntent{}, false, err
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `INSERT INTO source_write_intents
		(workspace_id, instance_key, task_id, request_id, request_json, idempotency_key, payload_json, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?)`, input.WorkspaceID, input.InstanceKey, input.TaskID, input.RequestID,
		input.PayloadJSON, input.IdempotencyKey, input.PayloadJSON, stamp, stamp)
	if err != nil {
		return sourceWriteIntent{}, false, err
	}
	input.Status = "pending"
	input.RequestJSON = input.PayloadJSON
	return input, false, nil
}

func (s *policyStore) SaveSourceWriteSnapshot(ctx context.Context, workspaceID, instanceKey, requestID, payloadJSON string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE source_write_intents SET payload_json = ?, status = 'dispatching', updated_at = ?
		WHERE workspace_id = ? AND instance_key = ? AND request_id = ?`, payloadJSON, time.Now().UTC().Format(time.RFC3339Nano), workspaceID, instanceKey, requestID)
	return err
}

func (s *policyStore) FinishSourceWrite(ctx context.Context, workspaceID, instanceKey, requestID, state, lastError, receiptJSON string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE source_write_intents SET status = ?, last_error = ?, receipt_json = ?, updated_at = ?
		WHERE workspace_id = ? AND instance_key = ? AND request_id = ?`, state, lastError, receiptJSON,
		time.Now().UTC().Format(time.RFC3339Nano), workspaceID, instanceKey, requestID)
	return err
}

func (s *policyStore) ListSourceWriteStatuses(ctx context.Context, workspaceID, instanceKey string) ([]sourceWriteStatus, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT task_id, request_id, status, receipt_json, last_error
		FROM source_write_intents WHERE workspace_id = ? AND instance_key = ? ORDER BY updated_at DESC`, workspaceID, instanceKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []sourceWriteStatus{}
	for rows.Next() {
		var item sourceWriteStatus
		if err := rows.Scan(&item.TaskID, &item.RequestID, &item.Status, &item.ReceiptJSON, &item.LastError); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *policyStore) GetSourceWriteIntent(ctx context.Context, workspaceID, instanceKey, requestID string) (sourceWriteIntent, error) {
	var item sourceWriteIntent
	err := s.db.QueryRowContext(ctx, `SELECT workspace_id, instance_key, task_id, request_id, request_json, idempotency_key,
		payload_json, status, receipt_json, last_error FROM source_write_intents
		WHERE workspace_id = ? AND instance_key = ? AND request_id = ?`, workspaceID, instanceKey, requestID).Scan(
		&item.WorkspaceID, &item.InstanceKey, &item.TaskID, &item.RequestID, &item.RequestJSON, &item.IdempotencyKey,
		&item.PayloadJSON, &item.Status, &item.ReceiptJSON, &item.LastError)
	return item, err
}

func (s *policyStore) SaveOutcomeReport(ctx context.Context, report outcomeReport) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO outcome_reports
		(workspace_id, instance_key, task_id, status, verified, cost_amount, cost_state, provenance, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(workspace_id, instance_key, task_id) DO UPDATE SET
		status = excluded.status, verified = excluded.verified, cost_amount = excluded.cost_amount,
		cost_state = excluded.cost_state, provenance = excluded.provenance, updated_at = excluded.updated_at`,
		report.WorkspaceID, report.InstanceKey, report.TaskID, report.Status, report.Verified, report.CostUSD,
		report.CostState, report.Provenance, report.UpdatedAt)
	return err
}

func (s *policyStore) ListOutcomeReports(ctx context.Context, workspaceID, instanceKey string) ([]outcomeReport, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT workspace_id, instance_key, task_id, status, verified, cost_amount, cost_state, provenance, updated_at
		FROM outcome_reports WHERE workspace_id = ? AND instance_key = ? ORDER BY updated_at DESC`, workspaceID, instanceKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	reports := []outcomeReport{}
	for rows.Next() {
		var report outcomeReport
		var verified int
		if err := rows.Scan(&report.WorkspaceID, &report.InstanceKey, &report.TaskID, &report.Status, &verified,
			&report.CostUSD, &report.CostState, &report.Provenance, &report.UpdatedAt); err != nil {
			return nil, err
		}
		report.Verified = verified != 0
		reports = append(reports, report)
	}
	return reports, rows.Err()
}

func (s *policyStore) SaveMemory(ctx context.Context, workspaceID, instanceKey, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO coordinator_memory (workspace_id, instance_key, memory_key, value, updated_at)
		VALUES (?, ?, ?, ?, ?) ON CONFLICT(workspace_id, instance_key, memory_key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		workspaceID, instanceKey, key, value, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *policyStore) ReadMemory(ctx context.Context, workspaceID, instanceKey, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM coordinator_memory WHERE workspace_id = ? AND instance_key = ? AND memory_key = ?`, workspaceID, instanceKey, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}
