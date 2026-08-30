package sqlite

// migrationsTraceMaintenance contains SQLite-specific indexes used by trace
// retention and emergency cleanup. Keep these separate from the original trace
// schema so existing Tiny installations receive them on upgrade.
var migrationsTraceMaintenance = []schemaMigration{
	{1000, `
		CREATE INDEX IF NOT EXISTS idx_transactions_project_end
			ON transactions(project_id, end_timestamp);
		CREATE INDEX IF NOT EXISTS idx_spans_project_transaction_event
			ON spans(project_id, transaction_event_id);
	`},
}
