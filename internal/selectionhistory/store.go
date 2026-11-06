// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

// Package selectionhistory stores GUI key-selection events and per-host defaults.
package selectionhistory

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jfut/ssh-keyselect/internal/identity"
	_ "github.com/ncruces/go-sqlite3/driver"
)

const (
	pageSizeLimit             = 200
	schemaVersion             = 1
	selectionOutcomeSelected  = "selected"
	selectionOutcomeCancelled = "cancelled"
	selectionOutcomeTimedOut  = "timed_out"
	selectionOutcomeNoKeys    = "no_keys"
	selectionOutcomeFailed    = "failed"
)

// Outcome names the result of a GUI picker request.
type Outcome string

const (
	OutcomeSelected  Outcome = selectionOutcomeSelected
	OutcomeCancelled Outcome = selectionOutcomeCancelled
	OutcomeTimedOut  Outcome = selectionOutcomeTimedOut
	OutcomeNoKeys    Outcome = selectionOutcomeNoKeys
	OutcomeFailed    Outcome = selectionOutcomeFailed
)

// HostHop snapshots one verified host key in the request's forwarding path.
type HostHop struct {
	Algorithm    string
	Fingerprint  string
	IsForwarding bool
	KnownHosts   []string
}

// Event is one completed GUI identity-selection request and its visible host path.
type Event struct {
	ID                     int64
	ConnectionID           string
	RequestedAt            time.Time
	DialogOpenedAt         time.Time
	ResolvedAt             time.Time
	SelectedAt             time.Time
	Outcome                Outcome
	TargetHostFingerprint  string
	SelectedKeyFingerprint string
	SelectedKeyAlgorithm   string
	SelectedKeyBlob        []byte
	SelectedKeyComment     string
	HostHops               []HostHop
}

// Store owns the local SQLite database for selection history and host defaults.
type Store struct {
	writeMu    sync.Mutex
	lifecycle  sync.Mutex
	activeCond *sync.Cond
	activeUses int
	closing    bool
	closeDone  chan struct{}
	closeErr   error
	db         *sql.DB
}

// Open creates or opens a private local history database and applies its schema.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("selection history database path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create selection history directory: %w", err)
	}
	dsn, err := databaseDSN(path)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open selection history database: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	store := &Store{db: db, closeDone: make(chan struct{})}
	store.activeCond = sync.NewCond(&store.lifecycle)
	if err := store.initialize(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set selection history database permissions: %w", err)
	}
	return store, nil
}

// databaseDSN quotes a filesystem path and applies connection-local SQLite safeguards.
func databaseDSN(path string) (string, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve selection history database path: %w", err)
	}
	databaseURL := url.URL{Scheme: "file"}
	if runtime.GOOS == "windows" {
		// Keep the drive letter at the start of the URI path. A hierarchical
		// file:///C:/ URI reaches Windows VFS as the invalid path /C:/... .
		databaseURL.Opaque = filepath.ToSlash(absolutePath)
	} else {
		databaseURL.Path = absolutePath
	}
	query := databaseURL.Query()
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "foreign_keys(1)")
	databaseURL.RawQuery = query.Encode()
	return databaseURL.String(), nil
}

// initialize enables WAL and applies the current schema version transactionally.
func (s *Store) initialize(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect to selection history database: %w", err)
	}
	var journalMode string
	if err := s.db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&journalMode); err != nil {
		return fmt.Errorf("enable selection history WAL: %w", err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		return fmt.Errorf("selection history database uses %q journal mode, want WAL", journalMode)
	}
	var currentVersion int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&currentVersion); err != nil {
		return fmt.Errorf("read selection history schema version: %w", err)
	}
	if currentVersion > schemaVersion {
		return fmt.Errorf("selection history schema version %d is newer than supported version %d", currentVersion, schemaVersion)
	}
	if currentVersion == schemaVersion {
		return nil
	}
	if currentVersion != 0 {
		return fmt.Errorf("selection history schema version %d is unsupported", currentVersion)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin selection history schema creation: %w", err)
	}
	statements := []string{
		`CREATE TABLE selection_events (
			id INTEGER PRIMARY KEY,
			connection_id TEXT NOT NULL,
			requested_at_ns INTEGER NOT NULL,
			dialog_opened_at_ns INTEGER,
			resolved_at_ns INTEGER NOT NULL,
			selected_at_ns INTEGER,
			outcome TEXT NOT NULL,
			target_host_fingerprint TEXT NOT NULL DEFAULT '',
			selected_key_fingerprint TEXT NOT NULL DEFAULT '',
			selected_key_algorithm TEXT NOT NULL DEFAULT '',
			selected_key_blob BLOB NOT NULL DEFAULT X'',
			selected_key_comment TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX selection_events_requested_at ON selection_events(requested_at_ns DESC, id DESC)`,
		`CREATE INDEX selection_events_target_host ON selection_events(target_host_fingerprint, requested_at_ns DESC, id DESC)`,
		`CREATE TABLE selection_host_hops (
			event_id INTEGER NOT NULL REFERENCES selection_events(id) ON DELETE CASCADE,
			ordinal INTEGER NOT NULL,
			key_algorithm TEXT NOT NULL,
			host_key_fingerprint TEXT NOT NULL,
			is_forwarding INTEGER NOT NULL CHECK (is_forwarding IN (0, 1)),
			known_hosts_hints TEXT NOT NULL,
			PRIMARY KEY (event_id, ordinal)
		)`,
		`CREATE TABLE selection_preferences (
			target_host_fingerprint TEXT PRIMARY KEY,
			selected_key_fingerprint TEXT NOT NULL,
			selected_at_ns INTEGER NOT NULL
		)`,
		`PRAGMA user_version = 1`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply selection history schema: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit selection history schema: %w", err)
	}
	return nil
}

// Retain keeps the database open until the returned release function is called.
func (s *Store) Retain() (func(), error) {
	if s == nil || s.db == nil {
		return nil, errors.New("selection history database is unavailable")
	}
	s.lifecycle.Lock()
	if s.closing && s.activeUses == 0 {
		s.lifecycle.Unlock()
		return nil, errors.New("selection history database is closing")
	}
	s.activeUses++
	s.lifecycle.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			s.lifecycle.Lock()
			s.activeUses--
			if s.closing && s.activeUses == 0 {
				s.activeCond.Broadcast()
			}
			s.lifecycle.Unlock()
		})
	}, nil
}

// Close waits for retained users before releasing SQLite's WAL and SHM handles.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	s.lifecycle.Lock()
	if s.closing {
		done := s.closeDone
		s.lifecycle.Unlock()
		<-done
		s.lifecycle.Lock()
		err := s.closeErr
		s.lifecycle.Unlock()
		return err
	}
	s.closing = true
	for s.activeUses > 0 {
		s.activeCond.Wait()
	}
	s.lifecycle.Unlock()

	err := s.db.Close()
	s.lifecycle.Lock()
	s.closeErr = err
	close(s.closeDone)
	s.lifecycle.Unlock()
	return err
}

// PreferredSelection returns the last explicitly selected key fingerprint for a target host.
func (s *Store) PreferredSelection(ctx context.Context, targetHostFingerprint string) (string, bool, error) {
	if s == nil || s.db == nil || targetHostFingerprint == "" {
		return "", false, nil
	}
	release, err := s.Retain()
	if err != nil {
		return "", false, err
	}
	defer release()
	var fingerprint string
	err = s.db.QueryRowContext(ctx,
		`SELECT selected_key_fingerprint FROM selection_preferences WHERE target_host_fingerprint = ?`,
		targetHostFingerprint,
	).Scan(&fingerprint)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read selection preference: %w", err)
	}
	return fingerprint, true, nil
}

// HasSelectionContext reports whether a picker selection exists for a target and forwarded path.
func (s *Store) HasSelectionContext(ctx context.Context, targetHostFingerprint string, forwardedFingerprints []string) (bool, error) {
	if s == nil || s.db == nil || targetHostFingerprint == "" {
		return false, nil
	}
	release, err := s.Retain()
	if err != nil {
		return false, err
	}
	defer release()
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, h.host_key_fingerprint, h.is_forwarding
		FROM selection_events AS e
		LEFT JOIN selection_host_hops AS h ON h.event_id = e.id
		WHERE e.target_host_fingerprint = ? AND e.outcome = ? AND e.selected_key_fingerprint != ''
		ORDER BY e.id DESC, h.ordinal`,
		targetHostFingerprint, string(OutcomeSelected),
	)
	if err != nil {
		return false, fmt.Errorf("read selection context from history: %w", err)
	}
	defer rows.Close()
	forwardingPathMatches := func(path []string) bool {
		if len(path) != len(forwardedFingerprints) {
			return false
		}
		for index := range path {
			if path[index] != forwardedFingerprints[index] {
				return false
			}
		}
		return true
	}
	var eventID int64
	var forwardingPath []string
	haveEvent := false
	for rows.Next() {
		var rowEventID int64
		var fingerprint sql.NullString
		var isForwarding sql.NullInt64
		if err := rows.Scan(&rowEventID, &fingerprint, &isForwarding); err != nil {
			return false, fmt.Errorf("read selection context row: %w", err)
		}
		if haveEvent && rowEventID != eventID {
			if forwardingPathMatches(forwardingPath) {
				return true, nil
			}
			forwardingPath = nil
		}
		if !haveEvent || rowEventID != eventID {
			eventID, haveEvent = rowEventID, true
		}
		if isForwarding.Valid && isForwarding.Int64 != 0 && fingerprint.Valid {
			forwardingPath = append(forwardingPath, fingerprint.String)
		}
	}
	if haveEvent && forwardingPathMatches(forwardingPath) {
		return true, nil
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("read selection contexts from history: %w", err)
	}
	return false, nil
}

// RememberSelection stores a selected key without creating a history event.
func (s *Store) RememberSelection(ctx context.Context, targetHostFingerprint, selectedKeyFingerprint string, selectedAt time.Time) error {
	if s == nil || s.db == nil {
		return errors.New("selection history database is unavailable")
	}
	if targetHostFingerprint == "" || selectedKeyFingerprint == "" || selectedAt.IsZero() {
		return errors.New("selection preference is incomplete")
	}
	release, err := s.Retain()
	if err != nil {
		return err
	}
	defer release()
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO selection_preferences (target_host_fingerprint, selected_key_fingerprint, selected_at_ns)
		VALUES (?, ?, ?)
		ON CONFLICT(target_host_fingerprint) DO UPDATE SET
			selected_key_fingerprint = excluded.selected_key_fingerprint,
			selected_at_ns = excluded.selected_at_ns`,
		targetHostFingerprint, selectedKeyFingerprint, selectedAt.UTC().UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("update selection preference: %w", err)
	}
	return nil
}

// Record appends one completed event and updates its host preference in one transaction.
func (s *Store) Record(ctx context.Context, event Event) error {
	return s.record(ctx, event, true)
}

// RecordHistory appends one event without reading or updating host preferences.
func (s *Store) RecordHistory(ctx context.Context, event Event) error {
	return s.record(ctx, event, false)
}

func (s *Store) record(ctx context.Context, event Event, rememberSelection bool) error {
	if s == nil || s.db == nil {
		return errors.New("selection history database is unavailable")
	}
	release, err := s.Retain()
	if err != nil {
		return err
	}
	defer release()
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if event.RequestedAt.IsZero() {
		event.RequestedAt = time.Now()
	}
	if event.ResolvedAt.IsZero() {
		event.ResolvedAt = time.Now()
	}
	if event.Outcome == "" {
		return errors.New("selection history outcome is empty")
	}
	if event.TargetHostFingerprint == "" && len(event.HostHops) > 0 {
		target := event.HostHops[len(event.HostHops)-1]
		if !target.IsForwarding {
			event.TargetHostFingerprint = target.Fingerprint
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin selection history write: %w", err)
	}
	defer tx.Rollback()
	var dialogOpenedAt, selectedAt any
	if !event.DialogOpenedAt.IsZero() {
		dialogOpenedAt = event.DialogOpenedAt.UTC().UnixNano()
	}
	if !event.SelectedAt.IsZero() {
		selectedAt = event.SelectedAt.UTC().UnixNano()
	}
	selectedKeyBlob := event.SelectedKeyBlob
	if selectedKeyBlob == nil {
		selectedKeyBlob = []byte{}
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO selection_events (
			connection_id, requested_at_ns, dialog_opened_at_ns, resolved_at_ns, selected_at_ns,
			outcome, target_host_fingerprint, selected_key_fingerprint,
			selected_key_algorithm, selected_key_blob, selected_key_comment
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ConnectionID, event.RequestedAt.UTC().UnixNano(), dialogOpenedAt,
		event.ResolvedAt.UTC().UnixNano(), selectedAt, string(event.Outcome),
		event.TargetHostFingerprint, event.SelectedKeyFingerprint,
		event.SelectedKeyAlgorithm, selectedKeyBlob, event.SelectedKeyComment,
	)
	if err != nil {
		return fmt.Errorf("insert selection history event: %w", err)
	}
	eventID, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("read selection history event ID: %w", err)
	}
	for ordinal, hop := range event.HostHops {
		hints, err := json.Marshal(hop.KnownHosts)
		if err != nil {
			return fmt.Errorf("encode known_hosts hints: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO selection_host_hops (
				event_id, ordinal, key_algorithm, host_key_fingerprint, is_forwarding, known_hosts_hints
			) VALUES (?, ?, ?, ?, ?, ?)`,
			eventID, ordinal, hop.Algorithm, hop.Fingerprint, hop.IsForwarding, string(hints),
		); err != nil {
			return fmt.Errorf("insert selection host hop: %w", err)
		}
	}
	if rememberSelection && event.Outcome == OutcomeSelected && event.TargetHostFingerprint != "" && event.SelectedKeyFingerprint != "" && !event.SelectedAt.IsZero() {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO selection_preferences (target_host_fingerprint, selected_key_fingerprint, selected_at_ns)
			VALUES (?, ?, ?)
			ON CONFLICT(target_host_fingerprint) DO UPDATE SET
				selected_key_fingerprint = excluded.selected_key_fingerprint,
				selected_at_ns = excluded.selected_at_ns`,
			event.TargetHostFingerprint, event.SelectedKeyFingerprint, event.SelectedAt.UTC().UnixNano(),
		); err != nil {
			return fmt.Errorf("update selection preference: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit selection history event: %w", err)
	}
	return nil
}

// DeleteHistory removes an event and its host path without touching preferences.
func (s *Store) DeleteHistory(ctx context.Context, eventID int64) error {
	if s == nil || s.db == nil {
		return errors.New("selection history database is unavailable")
	}
	release, err := s.Retain()
	if err != nil {
		return err
	}
	defer release()
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if eventID < 1 {
		return errors.New("selection history event ID is invalid")
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM selection_events WHERE id = ?`, eventID); err != nil {
		return fmt.Errorf("delete selection history event: %w", err)
	}
	return nil
}

// Delete removes a history event and restores the previous host preference when needed.
func (s *Store) Delete(ctx context.Context, eventID int64) error {
	if s == nil || s.db == nil {
		return errors.New("selection history database is unavailable")
	}
	release, err := s.Retain()
	if err != nil {
		return err
	}
	defer release()
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if eventID < 1 {
		return errors.New("selection history event ID is invalid")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin selection history delete: %w", err)
	}
	defer tx.Rollback()

	var targetHost, selectedKey string
	var selectedAt sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		SELECT target_host_fingerprint, selected_key_fingerprint, selected_at_ns
		FROM selection_events WHERE id = ?`, eventID,
	).Scan(&targetHost, &selectedKey, &selectedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return tx.Commit()
	}
	if err != nil {
		return fmt.Errorf("read selection history event to delete: %w", err)
	}
	if targetHost != "" && selectedKey != "" && selectedAt.Valid {
		var latestEventID int64
		err := tx.QueryRowContext(ctx, `
			SELECT id FROM selection_events
			WHERE target_host_fingerprint = ? AND outcome = ? AND selected_key_fingerprint != '' AND selected_at_ns IS NOT NULL
			ORDER BY id DESC LIMIT 1`, targetHost, selectionOutcomeSelected,
		).Scan(&latestEventID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("find latest host selection before delete: %w", err)
		}
		if latestEventID == eventID {
			result, err := tx.ExecContext(ctx, `
				DELETE FROM selection_preferences
				WHERE target_host_fingerprint = ? AND selected_key_fingerprint = ? AND selected_at_ns = ?`,
				targetHost, selectedKey, selectedAt.Int64,
			)
			if err != nil {
				return fmt.Errorf("delete host preference for removed event: %w", err)
			}
			deleted, err := result.RowsAffected()
			if err != nil {
				return fmt.Errorf("check deleted host preference: %w", err)
			}
			if deleted > 0 {
				var previousKey string
				var previousSelectedAt int64
				err := tx.QueryRowContext(ctx, `
					SELECT selected_key_fingerprint, selected_at_ns FROM selection_events
					WHERE target_host_fingerprint = ? AND id != ? AND outcome = ? AND selected_key_fingerprint != '' AND selected_at_ns IS NOT NULL
					ORDER BY id DESC LIMIT 1`, targetHost, eventID, selectionOutcomeSelected,
				).Scan(&previousKey, &previousSelectedAt)
				if err == nil {
					if _, err := tx.ExecContext(ctx, `
						INSERT INTO selection_preferences (target_host_fingerprint, selected_key_fingerprint, selected_at_ns)
						VALUES (?, ?, ?)
						ON CONFLICT(target_host_fingerprint) DO UPDATE SET
							selected_key_fingerprint = excluded.selected_key_fingerprint,
							selected_at_ns = excluded.selected_at_ns`,
						targetHost, previousKey, previousSelectedAt,
					); err != nil {
						return fmt.Errorf("restore previous host preference: %w", err)
					}
				} else if !errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("find previous host preference: %w", err)
				}
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM selection_events WHERE id = ?`, eventID); err != nil {
		return fmt.Errorf("delete selection history event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit selection history delete: %w", err)
	}
	return nil
}

// ListPage returns a stable request-time page and its total row count.
func (s *Store) ListPage(ctx context.Context, page, pageSize int) ([]Event, int, error) {
	return s.listEvents(ctx, page, pageSize, false)
}

// ListAll returns one consistent snapshot for sorting the complete history in the GUI.
func (s *Store) ListAll(ctx context.Context) ([]Event, error) {
	events, _, err := s.listEvents(ctx, 0, 0, true)
	return events, err
}

func (s *Store) listEvents(ctx context.Context, page, pageSize int, all bool) ([]Event, int, error) {
	if s == nil || s.db == nil {
		return nil, 0, errors.New("selection history database is unavailable")
	}
	release, err := s.Retain()
	if err != nil {
		return nil, 0, err
	}
	defer release()
	if !all && (page < 0 || pageSize < 1 || pageSize > pageSizeLimit) {
		return nil, 0, errors.New("invalid selection history page")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, fmt.Errorf("begin selection history read: %w", err)
	}
	defer tx.Rollback()
	var total int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM selection_events`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count selection history events: %w", err)
	}
	query := `
		WITH page_events AS (
			SELECT id, connection_id, requested_at_ns, dialog_opened_at_ns, resolved_at_ns, selected_at_ns,
				outcome, target_host_fingerprint, selected_key_fingerprint,
				selected_key_algorithm, selected_key_blob, selected_key_comment
			FROM selection_events
			ORDER BY requested_at_ns DESC, id DESC`
	var args []any
	if !all {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, pageSize, page*pageSize)
	}
	query += `
		)
		SELECT e.id, e.connection_id, e.requested_at_ns, e.dialog_opened_at_ns, e.resolved_at_ns,
			e.selected_at_ns, e.outcome, e.target_host_fingerprint, e.selected_key_fingerprint,
			e.selected_key_algorithm, e.selected_key_blob, e.selected_key_comment,
			h.ordinal, h.key_algorithm, h.host_key_fingerprint, h.is_forwarding, h.known_hosts_hints
		FROM page_events e
		LEFT JOIN selection_host_hops h ON h.event_id = e.id
		ORDER BY e.requested_at_ns DESC, e.id DESC, h.ordinal`
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list selection history events: %w", err)
	}
	var events []Event
	var current *Event
	for rows.Next() {
		var event Event
		var requestedAt, resolvedAt int64
		var dialogOpenedAt, selectedAt sql.NullInt64
		var hopOrdinal, isForwarding sql.NullInt64
		var hopAlgorithm, hopFingerprint, hints sql.NullString
		if err := rows.Scan(
			&event.ID, &event.ConnectionID, &requestedAt, &dialogOpenedAt, &resolvedAt, &selectedAt,
			&event.Outcome, &event.TargetHostFingerprint, &event.SelectedKeyFingerprint,
			&event.SelectedKeyAlgorithm, &event.SelectedKeyBlob, &event.SelectedKeyComment,
			&hopOrdinal, &hopAlgorithm, &hopFingerprint, &isForwarding, &hints,
		); err != nil {
			_ = rows.Close()
			return nil, 0, fmt.Errorf("read selection history event: %w", err)
		}
		if current == nil || current.ID != event.ID {
			event.RequestedAt = time.Unix(0, requestedAt).UTC()
			event.ResolvedAt = time.Unix(0, resolvedAt).UTC()
			if dialogOpenedAt.Valid {
				event.DialogOpenedAt = time.Unix(0, dialogOpenedAt.Int64).UTC()
			}
			if selectedAt.Valid {
				event.SelectedAt = time.Unix(0, selectedAt.Int64).UTC()
			}
			event.HostHops = make([]HostHop, 0)
			events = append(events, event)
			current = &events[len(events)-1]
		}
		if hopOrdinal.Valid {
			var knownHosts []string
			if err := json.Unmarshal([]byte(hints.String), &knownHosts); err != nil {
				_ = rows.Close()
				return nil, 0, fmt.Errorf("decode selection host hints: %w", err)
			}
			current.HostHops = append(current.HostHops, HostHop{
				Algorithm: hopAlgorithm.String, Fingerprint: hopFingerprint.String,
				IsForwarding: isForwarding.Int64 != 0, KnownHosts: knownHosts,
			})
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, 0, fmt.Errorf("read selection history events: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, 0, fmt.Errorf("close selection history event rows: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, fmt.Errorf("finish selection history read: %w", err)
	}
	return events, total, nil
}

// ExportCSV writes every event in request-time order, including its full host path as JSON.
func (s *Store) ExportCSV(ctx context.Context, destination io.Writer) error {
	if s == nil || s.db == nil {
		return errors.New("selection history database is unavailable")
	}
	release, err := s.Retain()
	if err != nil {
		return err
	}
	defer release()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin selection history export: %w", err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
		SELECT e.id, e.connection_id, e.requested_at_ns, e.dialog_opened_at_ns, e.resolved_at_ns,
			e.selected_at_ns, e.outcome, e.target_host_fingerprint, e.selected_key_fingerprint,
			e.selected_key_algorithm, e.selected_key_blob, e.selected_key_comment,
			h.ordinal, h.key_algorithm, h.host_key_fingerprint, h.is_forwarding, h.known_hosts_hints
		FROM selection_events e
		LEFT JOIN selection_host_hops h ON h.event_id = e.id
		ORDER BY e.requested_at_ns DESC, e.id DESC, h.ordinal`,
	)
	if err != nil {
		return fmt.Errorf("query selection history export: %w", err)
	}
	writer := csv.NewWriter(destination)
	if err := writer.Write([]string{
		"Requested (Local)", "Target host", "Target host key", "Forwarded", "Key comment", "Key",
		"Fingerprint", "Selected at (Local)", "Result", "Event ID", "Connection ID",
		"Dialog opened (Local)", "Resolved (Local)", "Target host fingerprint", "Host path (JSON)",
		"Selected key algorithm", "Selected public key (Base64)",
	}); err != nil {
		_ = rows.Close()
		return fmt.Errorf("write selection history CSV header: %w", err)
	}
	var current *Event
	flush := func() error {
		if current == nil {
			return nil
		}
		path, err := json.Marshal(current.HostHops)
		if err != nil {
			return fmt.Errorf("encode exported host path: %w", err)
		}
		return writer.Write([]string{
			csvHistoryTime(current.RequestedAt), csvTargetHostName(*current), csvTargetHostKey(*current),
			csvForwardedLabel(current.HostHops), csvSelectedKeyComment(*current), csvSelectedKey(*current),
			csvHistoryValue(current.SelectedKeyFingerprint), csvHistoryTime(current.SelectedAt),
			csvOutcomeLabel(current.Outcome), strconv.FormatInt(current.ID, 10),
			csvHistoryValue(current.ConnectionID), csvHistoryTime(current.DialogOpenedAt),
			csvHistoryTime(current.ResolvedAt), csvHistoryValue(current.TargetHostFingerprint),
			string(path), csvHistoryValue(current.SelectedKeyAlgorithm), csvSelectedKeyBase64(*current),
		})
	}
	for rows.Next() {
		var event Event
		var requestedAt, resolvedAt int64
		var dialogOpenedAt, selectedAt sql.NullInt64
		var hopOrdinal sql.NullInt64
		var hopAlgorithm, hopFingerprint, hints sql.NullString
		var isForwarding sql.NullInt64
		if err := rows.Scan(
			&event.ID, &event.ConnectionID, &requestedAt, &dialogOpenedAt, &resolvedAt,
			&selectedAt, &event.Outcome, &event.TargetHostFingerprint, &event.SelectedKeyFingerprint,
			&event.SelectedKeyAlgorithm, &event.SelectedKeyBlob, &event.SelectedKeyComment, &hopOrdinal, &hopAlgorithm,
			&hopFingerprint, &isForwarding, &hints,
		); err != nil {
			_ = rows.Close()
			return fmt.Errorf("read selection history export row: %w", err)
		}
		if current == nil || current.ID != event.ID {
			if err := flush(); err != nil {
				_ = rows.Close()
				return fmt.Errorf("write selection history CSV row: %w", err)
			}
			event.RequestedAt = time.Unix(0, requestedAt).UTC()
			event.ResolvedAt = time.Unix(0, resolvedAt).UTC()
			if dialogOpenedAt.Valid {
				event.DialogOpenedAt = time.Unix(0, dialogOpenedAt.Int64).UTC()
			}
			if selectedAt.Valid {
				event.SelectedAt = time.Unix(0, selectedAt.Int64).UTC()
			}
			event.HostHops = make([]HostHop, 0)
			current = &event
		}
		if hopOrdinal.Valid {
			var knownHosts []string
			if err := json.Unmarshal([]byte(hints.String), &knownHosts); err != nil {
				_ = rows.Close()
				return fmt.Errorf("decode exported host hints: %w", err)
			}
			current.HostHops = append(current.HostHops, HostHop{
				Algorithm: hopAlgorithm.String, Fingerprint: hopFingerprint.String,
				IsForwarding: isForwarding.Int64 != 0, KnownHosts: knownHosts,
			})
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("read selection history export: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close selection history export rows: %w", err)
	}
	if err := flush(); err != nil {
		return fmt.Errorf("write final selection history CSV row: %w", err)
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("flush selection history CSV: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("finish selection history export: %w", err)
	}
	return nil
}

func formatCSVTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Local().Format(time.RFC3339Nano)
}

func csvHistoryTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return formatCSVTime(value)
}

func csvHistoryValue(value string) string {
	if safe := identity.DisplayText(value); safe != "" {
		return csvSafeText(safe)
	}
	return "-"
}

func csvTargetHostName(event Event) string {
	if event.TargetHostFingerprint == "" || len(event.HostHops) == 0 {
		return "-"
	}
	target := event.HostHops[len(event.HostHops)-1]
	if target.IsForwarding {
		return "-"
	}
	var names []string
	for _, name := range target.KnownHosts {
		if safe := identity.DisplayText(name); safe != "" {
			names = append(names, safe)
		}
	}
	if len(names) == 0 {
		return "-"
	}
	return csvSafeText(strings.Join(names, ", "))
}

func csvTargetHostKey(event Event) string {
	if event.TargetHostFingerprint == "" {
		return "-"
	}
	fingerprint := identity.DisplayText(event.TargetHostFingerprint)
	for index := len(event.HostHops) - 1; index >= 0; index-- {
		hop := event.HostHops[index]
		if hop.IsForwarding || hop.Fingerprint != event.TargetHostFingerprint {
			continue
		}
		if algorithm := identity.DisplayText(hop.Algorithm); algorithm != "" {
			return csvSafeText(algorithm + " " + fingerprint)
		}
		break
	}
	return csvSafeText(fingerprint)
}

func csvForwardedLabel(hops []HostHop) string {
	var forwarded []string
	for _, hop := range hops {
		if !hop.IsForwarding {
			continue
		}
		key := identity.DisplayText(hop.Fingerprint)
		algorithm := identity.DisplayText(hop.Algorithm)
		if key == "" {
			key = "-"
		} else if algorithm != "" {
			key = algorithm + " " + key
		}
		var names []string
		for _, name := range hop.KnownHosts {
			if safe := identity.DisplayText(name); safe != "" {
				names = append(names, safe)
			}
		}
		label := key
		if len(names) > 0 {
			label = strings.Join(names, ", ")
			if key != "-" {
				label += " [" + key + "]"
			}
		}
		forwarded = append(forwarded, csvSafeText(label))
	}
	if len(forwarded) == 0 {
		return "-"
	}
	return strings.Join(forwarded, " -> ")
}

func csvSelectedKeyComment(event Event) string {
	if event.SelectedKeyFingerprint == "" {
		return "-"
	}
	return csvHistoryValue(event.SelectedKeyComment)
}

func csvSelectedKey(event Event) string {
	if event.SelectedKeyFingerprint == "" || len(event.SelectedKeyBlob) == 0 {
		return "-"
	}
	algorithm := identity.DisplayText(event.SelectedKeyAlgorithm)
	key := csvSelectedKeyBase64(event)
	if algorithm == "" {
		return key
	}
	return csvSafeText(algorithm + " " + key)
}

func csvSelectedKeyBase64(event Event) string {
	if event.SelectedKeyFingerprint == "" || len(event.SelectedKeyBlob) == 0 {
		return "-"
	}
	return base64.StdEncoding.EncodeToString(event.SelectedKeyBlob)
}

func csvOutcomeLabel(outcome Outcome) string {
	switch outcome {
	case OutcomeSelected:
		return "Selected"
	case OutcomeCancelled:
		return "Cancelled"
	case OutcomeTimedOut:
		return "Timed out"
	case OutcomeNoKeys:
		return "No keys"
	default:
		return "Failed"
	}
}

// csvSafeText sanitizes display controls and neutralizes spreadsheet formula prefixes.
func csvSafeText(value string) string {
	value = identity.DisplayText(value)
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + value
	}
	return value
}
