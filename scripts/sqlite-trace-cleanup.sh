#!/usr/bin/env bash
set -euo pipefail

usage() {
	cat <<'EOF'
Usage: sqlite-trace-cleanup.sh <database.sqlite> <keep-days> [batch-size] [--vacuum]

Deletes performance transactions older than <keep-days> and their child spans
in bounded batches. A SQLite backup is created before any destructive work.

Examples:
  bash scripts/sqlite-trace-cleanup.sh /var/lib/urgentry/urgentry.db 7
  bash scripts/sqlite-trace-cleanup.sh /var/lib/urgentry/urgentry.db 14 2000 --vacuum

Stop Urgentry before running this script. VACUUM is optional because it can take
considerable time and needs free disk space roughly comparable to the database.
EOF
}

if [[ $# -lt 2 || $# -gt 4 ]]; then
	usage
	exit 1
fi

DB="$1"
KEEP_DAYS="$2"
BATCH_SIZE="${3:-2000}"
VACUUM="${4:-}"

if [[ ! -f "$DB" ]]; then
	echo "Database does not exist: $DB" >&2
	exit 1
fi

if ! command -v sqlite3 >/dev/null 2>&1; then
	echo "sqlite3 is required." >&2
	exit 1
fi

if ! [[ "$KEEP_DAYS" =~ ^[0-9]+$ ]] || (( KEEP_DAYS < 1 )); then
	echo "keep-days must be a positive integer." >&2
	exit 1
fi

if ! [[ "$BATCH_SIZE" =~ ^[0-9]+$ ]] || (( BATCH_SIZE < 1 || BATCH_SIZE > 10000 )); then
	echo "batch-size must be between 1 and 10000." >&2
	exit 1
fi

if [[ -n "$VACUUM" && "$VACUUM" != "--vacuum" ]]; then
	usage
	exit 1
fi

BACKUP="${DB}.trace-cleanup.$(date -u +%Y%m%dT%H%M%SZ).bak"
CUTOFF="$(sqlite3 "$DB" "SELECT strftime('%Y-%m-%dT%H:%M:%SZ','now','-${KEEP_DAYS} days');")"

if [[ -z "$CUTOFF" ]]; then
	echo "Could not calculate cleanup cutoff." >&2
	exit 1
fi

echo "Database:    $DB"
echo "Backup:      $BACKUP"
echo "Keep:        $KEEP_DAYS days"
echo "Cutoff:      $CUTOFF"
echo "Batch size:  $BATCH_SIZE"
echo
echo "IMPORTANT: Urgentry should be stopped before continuing."

sqlite3 "$DB" ".timeout 60000" ".backup '$BACKUP'"

echo "Backup complete."

sqlite3 "$DB" <<'SQL'
.timeout 60000
PRAGMA busy_timeout = 60000;
CREATE INDEX IF NOT EXISTS idx_transactions_project_end ON transactions(project_id, end_timestamp);
CREATE INDEX IF NOT EXISTS idx_spans_project_transaction_event ON spans(project_id, transaction_event_id);
ANALYZE;
SQL

BEFORE_TX="$(sqlite3 "$DB" "SELECT COUNT(*) FROM transactions;")"
BEFORE_SPANS="$(sqlite3 "$DB" "SELECT COUNT(*) FROM spans;")"
OLD_TX="$(sqlite3 "$DB" "SELECT COUNT(*) FROM transactions WHERE end_timestamp < '$CUTOFF';")"

echo "Transactions before: $BEFORE_TX ($OLD_TX eligible)"
echo "Spans before:        $BEFORE_SPANS"

TOTAL_DELETED=0
BATCH=0

while true; do
	DELETED="$(sqlite3 "$DB" <<SQL
.timeout 60000
PRAGMA busy_timeout = 60000;
BEGIN IMMEDIATE;
CREATE TEMP TABLE _trace_cleanup_batch (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	event_id TEXT NOT NULL
);
INSERT INTO _trace_cleanup_batch (id, project_id, event_id)
SELECT id, project_id, event_id
FROM transactions
WHERE end_timestamp < '$CUTOFF'
ORDER BY end_timestamp ASC
LIMIT $BATCH_SIZE;
DELETE FROM spans
WHERE (project_id, transaction_event_id) IN (
	SELECT project_id, event_id FROM _trace_cleanup_batch
);
DELETE FROM transactions
WHERE id IN (SELECT id FROM _trace_cleanup_batch);
SELECT changes();
COMMIT;
SQL
)"

	DELETED="$(printf '%s\n' "$DELETED" | tail -n 1 | tr -d '[:space:]')"
	if ! [[ "$DELETED" =~ ^[0-9]+$ ]]; then
		echo "Unexpected sqlite3 result: $DELETED" >&2
		exit 1
	fi
	if (( DELETED == 0 )); then
		break
	fi

	((BATCH += 1))
	((TOTAL_DELETED += DELETED))
	echo "Batch $BATCH: deleted $DELETED transactions ($TOTAL_DELETED total)"
done

sqlite3 "$DB" <<'SQL'
.timeout 60000
PRAGMA busy_timeout = 60000;
ANALYZE;
PRAGMA optimize;
PRAGMA wal_checkpoint(TRUNCATE);
SQL

AFTER_TX="$(sqlite3 "$DB" "SELECT COUNT(*) FROM transactions;")"
AFTER_SPANS="$(sqlite3 "$DB" "SELECT COUNT(*) FROM spans;")"

echo
echo "Transactions after:  $AFTER_TX"
echo "Spans after:         $AFTER_SPANS"
echo "Transactions deleted: $TOTAL_DELETED"

if [[ "$VACUUM" == "--vacuum" ]]; then
	echo "Running VACUUM..."
	sqlite3 "$DB" ".timeout 60000" "VACUUM;"
fi

echo "Cleanup complete. Backup retained at: $BACKUP"
