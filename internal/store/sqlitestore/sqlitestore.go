// Package sqlitestore is the SQLite-backed implementation of store.Store.
// It is the only place that imports a SQL driver. Everything else sits
// above the store.Reader/Writer interfaces.
package sqlitestore

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/anttti/j/internal/model"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Store struct {
	db *sql.DB
}

// Open opens (or creates) a SQLite database at path and runs migrations.
// Use ":memory:" for tests.
func Open(path string) (*Store, error) {
	dsn := path
	if path != ":memory:" {
		dsn = path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// In-memory DB must share a single connection so migrations survive.
	if path == ":memory:" {
		db.SetMaxOpenConns(1)
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
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	for _, name := range files {
		b, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if _, err := s.db.Exec(string(b)); err != nil {
			return fmt.Errorf("migrate %s: %w", name, err)
		}
	}
	return nil
}

// ---------- Reader ----------

func (s *Store) Get(ctx context.Context, key string) (*model.Issue, error) {
	row := s.db.QueryRowContext(ctx, selectIssueFrom("issues")+" WHERE UPPER(issues.key) = UPPER(?) LIMIT 1", key)
	iss, err := scanIssue(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &iss, nil
}

func (s *Store) List(ctx context.Context, f model.Filter, p model.Page) ([]model.Issue, int, error) {
	from, where, args := buildQuery(f)
	countSQL := "SELECT COUNT(*) FROM " + from + " " + where
	var total int
	if err := s.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count: %w", err)
	}
	q := selectIssueFrom(from) + " " + where + " ORDER BY issues.updated DESC"
	if p.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d OFFSET %d", p.Limit, p.Offset)
	} else if p.Offset > 0 {
		q += fmt.Sprintf(" LIMIT -1 OFFSET %d", p.Offset)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list: %w", err)
	}
	defer rows.Close()
	var out []model.Issue
	for rows.Next() {
		iss, err := scanIssue(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, iss)
	}
	return out, total, rows.Err()
}

func (s *Store) Comments(ctx context.Context, key string) ([]model.Comment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, issue_key, author_name, author_account_id, author_email, body, created, updated
		   FROM comments
		  WHERE UPPER(issue_key) = UPPER(?)
		  ORDER BY created ASC`, key)
	if err != nil {
		return nil, fmt.Errorf("comments: %w", err)
	}
	defer rows.Close()
	var out []model.Comment
	for rows.Next() {
		var (
			c                     model.Comment
			name, accID, email    sql.NullString
			created, updatedStr   string
		)
		if err := rows.Scan(&c.ID, &c.IssueKey, &name, &accID, &email, &c.Body, &created, &updatedStr); err != nil {
			return nil, err
		}
		if name.Valid || accID.Valid || email.Valid {
			c.Author = &model.User{DisplayName: name.String, AccountID: accID.String, Email: email.String}
		}
		c.Created = mustParseTime(created)
		c.Updated = mustParseTime(updatedStr)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) DistinctTypes(ctx context.Context) ([]string, error) {
	return s.distinctCol(ctx, "issue_type")
}

func (s *Store) DistinctStatuses(ctx context.Context) ([]string, error) {
	return s.distinctCol(ctx, "status")
}

func (s *Store) distinctCol(ctx context.Context, col string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf("SELECT DISTINCT %s FROM issues WHERE %s != '' ORDER BY %s", col, col, col))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) Assignees(ctx context.Context) ([]model.User, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT assignee_account_id, MAX(assignee_name), MAX(COALESCE(assignee_email, ''))
		  FROM issues
		 WHERE assignee_account_id IS NOT NULL AND assignee_account_id != ''
		 GROUP BY assignee_account_id
		 ORDER BY MAX(assignee_name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.User
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.AccountID, &u.DisplayName, &u.Email); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) SyncState(ctx context.Context) (model.SyncState, error) {
	var (
		ss                  model.SyncState
		lastSync, lastFull  sql.NullString
		lastErr             string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT last_sync_utc, last_full_sync, last_error FROM sync_state WHERE id=1`).
		Scan(&lastSync, &lastFull, &lastErr)
	if err != nil {
		return ss, err
	}
	if lastSync.Valid {
		ss.LastSyncUTC = mustParseTime(lastSync.String)
	}
	if lastFull.Valid {
		ss.LastFullSyncAt = mustParseTime(lastFull.String)
	}
	ss.LastError = lastErr
	return ss, nil
}

// ---------- Writer ----------

func (s *Store) UpsertIssue(ctx context.Context, iss model.Issue, rawJSON []byte) error {
	labels, _ := json.Marshal(iss.Labels)
	var (
		aName, aID, aEmail sql.NullString
		rName, rID, rEmail sql.NullString
		dueDate            sql.NullString
	)
	if iss.Assignee != nil {
		aName = sql.NullString{String: iss.Assignee.DisplayName, Valid: true}
		aID = sql.NullString{String: iss.Assignee.AccountID, Valid: true}
		aEmail = sql.NullString{String: iss.Assignee.Email, Valid: true}
	}
	if iss.Reporter != nil {
		rName = sql.NullString{String: iss.Reporter.DisplayName, Valid: true}
		rID = sql.NullString{String: iss.Reporter.AccountID, Valid: true}
		rEmail = sql.NullString{String: iss.Reporter.Email, Valid: true}
	}
	if iss.DueDate != nil {
		dueDate = sql.NullString{String: iss.DueDate.Format("2006-01-02"), Valid: true}
	}
	if len(rawJSON) == 0 {
		rawJSON = []byte("{}")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO issues (
			key, id, project_key, summary, description,
			status, status_cat, issue_type, priority,
			assignee_name, assignee_account_id, assignee_email,
			reporter_name, reporter_account_id, reporter_email,
			labels_json, due_date, created, updated, url, raw_json
		) VALUES (?,?,?,?,?, ?,?,?,?, ?,?,?, ?,?,?, ?,?,?,?,?,?)
		ON CONFLICT(key) DO UPDATE SET
			id=excluded.id, project_key=excluded.project_key,
			summary=excluded.summary, description=excluded.description,
			status=excluded.status, status_cat=excluded.status_cat,
			issue_type=excluded.issue_type, priority=excluded.priority,
			assignee_name=excluded.assignee_name,
			assignee_account_id=excluded.assignee_account_id,
			assignee_email=excluded.assignee_email,
			reporter_name=excluded.reporter_name,
			reporter_account_id=excluded.reporter_account_id,
			reporter_email=excluded.reporter_email,
			labels_json=excluded.labels_json, due_date=excluded.due_date,
			created=excluded.created, updated=excluded.updated,
			url=excluded.url, raw_json=excluded.raw_json
	`,
		iss.Key, iss.ID, iss.ProjectKey, iss.Summary, iss.Description,
		iss.Status, iss.StatusCategory, iss.Type, iss.Priority,
		aName, aID, aEmail,
		rName, rID, rEmail,
		string(labels), dueDate,
		iss.Created.UTC().Format(time.RFC3339Nano),
		iss.Updated.UTC().Format(time.RFC3339Nano),
		iss.URL, rawJSON)
	if err != nil {
		return fmt.Errorf("upsert issue: %w", err)
	}
	return nil
}

func (s *Store) ReplaceComments(ctx context.Context, issueKey string, cs []model.Comment) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM comments WHERE UPPER(issue_key) = UPPER(?)`, issueKey); err != nil {
		return fmt.Errorf("delete comments: %w", err)
	}
	for _, c := range cs {
		var aName, aID, aEmail sql.NullString
		if c.Author != nil {
			aName = sql.NullString{String: c.Author.DisplayName, Valid: true}
			aID = sql.NullString{String: c.Author.AccountID, Valid: true}
			aEmail = sql.NullString{String: c.Author.Email, Valid: true}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO comments (id, issue_key, author_name, author_account_id, author_email, body, created, updated)
			VALUES (?,?,?,?,?,?,?,?)`,
			c.ID, c.IssueKey, aName, aID, aEmail, c.Body,
			c.Created.UTC().Format(time.RFC3339Nano),
			c.Updated.UTC().Format(time.RFC3339Nano),
		); err != nil {
			return fmt.Errorf("insert comment %s: %w", c.ID, err)
		}
	}
	return tx.Commit()
}

func (s *Store) SetSyncState(ctx context.Context, ss model.SyncState) error {
	var lastSync, lastFull sql.NullString
	if !ss.LastSyncUTC.IsZero() {
		lastSync = sql.NullString{String: ss.LastSyncUTC.UTC().Format(time.RFC3339Nano), Valid: true}
	}
	if !ss.LastFullSyncAt.IsZero() {
		lastFull = sql.NullString{String: ss.LastFullSyncAt.UTC().Format(time.RFC3339Nano), Valid: true}
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE sync_state SET last_sync_utc=?, last_full_sync=?, last_error=? WHERE id=1`,
		lastSync, lastFull, ss.LastError)
	return err
}

// ---------- internals ----------

// selectIssueFrom produces the SELECT-list with an explicit FROM clause so
// we can transparently JOIN issues_fts for search queries.
func selectIssueFrom(from string) string {
	return `
SELECT issues.key, issues.id, issues.project_key, issues.summary, issues.description,
       issues.status, issues.status_cat, issues.issue_type, issues.priority,
       issues.assignee_name, issues.assignee_account_id, issues.assignee_email,
       issues.reporter_name, issues.reporter_account_id, issues.reporter_email,
       issues.labels_json, issues.due_date, issues.created, issues.updated, issues.url
  FROM ` + from
}

type scanner interface {
	Scan(dest ...any) error
}

func scanIssue(sc scanner) (model.Issue, error) {
	var (
		iss                model.Issue
		descr, statusCat   string
		priority           string
		aName, aID, aEmail sql.NullString
		rName, rID, rEmail sql.NullString
		labels, dueDate    sql.NullString
		created, updated   string
	)
	err := sc.Scan(
		&iss.Key, &iss.ID, &iss.ProjectKey, &iss.Summary, &descr,
		&iss.Status, &statusCat, &iss.Type, &priority,
		&aName, &aID, &aEmail,
		&rName, &rID, &rEmail,
		&labels, &dueDate, &created, &updated, &iss.URL,
	)
	if err != nil {
		return iss, err
	}
	iss.Description = descr
	iss.StatusCategory = statusCat
	iss.Priority = priority
	if aID.Valid || aName.Valid {
		iss.Assignee = &model.User{AccountID: aID.String, DisplayName: aName.String, Email: aEmail.String}
	}
	if rID.Valid || rName.Valid {
		iss.Reporter = &model.User{AccountID: rID.String, DisplayName: rName.String, Email: rEmail.String}
	}
	if labels.Valid && labels.String != "" {
		_ = json.Unmarshal([]byte(labels.String), &iss.Labels)
	}
	if dueDate.Valid && dueDate.String != "" {
		if t, err := time.Parse("2006-01-02", dueDate.String); err == nil {
			iss.DueDate = &t
		}
	}
	iss.Created = mustParseTime(created)
	iss.Updated = mustParseTime(updated)
	return iss, nil
}

// buildQuery returns the FROM clause, WHERE clause, and arg list for a
// filter. Search is implemented via a JOIN against the FTS5 index.
func buildQuery(f model.Filter) (string, string, []any) {
	from := "issues"
	var parts []string
	var args []any

	if f.Search != "" {
		from = "issues JOIN issues_fts ON issues_fts.rowid = issues.rowid"
		parts = append(parts, "issues_fts MATCH ?")
		args = append(args, ftsQuery(f.Search))
	}
	if len(f.Types) > 0 {
		parts = append(parts, "issues.issue_type IN ("+placeholders(len(f.Types))+")")
		for _, v := range f.Types {
			args = append(args, v)
		}
	}
	if len(f.Statuses) > 0 {
		parts = append(parts, "issues.status IN ("+placeholders(len(f.Statuses))+")")
		for _, v := range f.Statuses {
			args = append(args, v)
		}
	}
	switch f.Assignee.Kind {
	case model.AssigneeKindUnassigned:
		parts = append(parts, "(issues.assignee_account_id IS NULL OR issues.assignee_account_id = '')")
	case model.AssigneeKindAccount:
		parts = append(parts, "issues.assignee_account_id = ?")
		args = append(args, f.Assignee.AccountID)
	case model.AssigneeKindMe:
		parts = append(parts, "1=0") // resolver bug: should be rewritten above
	}
	where := ""
	if len(parts) > 0 {
		where = "WHERE " + strings.Join(parts, " AND ")
	}
	return from, where, args
}

// ftsQuery turns user input into an FTS5 query with prefix matching on each
// whitespace-separated term. Double quotes are stripped; other special
// chars are fine inside the double-quoted term.
func ftsQuery(s string) string {
	fields := strings.Fields(s)
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.ReplaceAll(f, `"`, "")
		if f == "" {
			continue
		}
		parts = append(parts, `"`+f+`"*`)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	p := make([]string, n)
	for i := range p {
		p[i] = "?"
	}
	return strings.Join(p, ",")
}

func mustParseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	// Try RFC3339Nano first, then RFC3339.
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	return time.Time{}
}
