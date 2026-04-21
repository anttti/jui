-- Schema v1.
CREATE TABLE issues (
  key                 TEXT PRIMARY KEY,
  id                  TEXT NOT NULL,
  project_key         TEXT NOT NULL,
  summary             TEXT NOT NULL,
  description         TEXT NOT NULL DEFAULT '',
  status              TEXT NOT NULL DEFAULT '',
  status_cat          TEXT NOT NULL DEFAULT '',
  issue_type          TEXT NOT NULL DEFAULT '',
  priority            TEXT NOT NULL DEFAULT '',
  assignee_name       TEXT,
  assignee_account_id TEXT,
  assignee_email      TEXT,
  reporter_name       TEXT,
  reporter_account_id TEXT,
  reporter_email      TEXT,
  labels_json         TEXT NOT NULL DEFAULT '[]',
  due_date            TEXT,
  created             TEXT NOT NULL,
  updated             TEXT NOT NULL,
  url                 TEXT NOT NULL,
  raw_json            BLOB NOT NULL
);

CREATE INDEX idx_issues_updated  ON issues(updated DESC);
CREATE INDEX idx_issues_status   ON issues(status);
CREATE INDEX idx_issues_type     ON issues(issue_type);
CREATE INDEX idx_issues_assignee ON issues(assignee_account_id);

CREATE TABLE comments (
  id                TEXT PRIMARY KEY,
  issue_key         TEXT NOT NULL,
  author_name       TEXT,
  author_account_id TEXT,
  author_email      TEXT,
  body              TEXT NOT NULL DEFAULT '',
  created           TEXT NOT NULL,
  updated           TEXT NOT NULL
);
CREATE INDEX idx_comments_issue ON comments(issue_key, created);

CREATE VIRTUAL TABLE issues_fts USING fts5(
  summary,
  description,
  content='issues',
  content_rowid='rowid',
  tokenize = 'unicode61'
);

CREATE TRIGGER issues_ai AFTER INSERT ON issues BEGIN
  INSERT INTO issues_fts(rowid, summary, description)
  VALUES (new.rowid, new.summary, new.description);
END;

CREATE TRIGGER issues_ad AFTER DELETE ON issues BEGIN
  INSERT INTO issues_fts(issues_fts, rowid, summary, description)
  VALUES ('delete', old.rowid, old.summary, old.description);
END;

CREATE TRIGGER issues_au AFTER UPDATE ON issues BEGIN
  INSERT INTO issues_fts(issues_fts, rowid, summary, description)
  VALUES ('delete', old.rowid, old.summary, old.description);
  INSERT INTO issues_fts(rowid, summary, description)
  VALUES (new.rowid, new.summary, new.description);
END;

CREATE TABLE sync_state (
  id              INTEGER PRIMARY KEY CHECK (id = 1),
  last_sync_utc   TEXT,
  last_full_sync  TEXT,
  last_error      TEXT NOT NULL DEFAULT ''
);
INSERT INTO sync_state(id) VALUES (1);
