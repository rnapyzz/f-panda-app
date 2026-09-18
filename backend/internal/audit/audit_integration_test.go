package audit_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/rnapyzz/f-panda-app/backend/internal/audit"
	"github.com/rnapyzz/f-panda-app/backend/internal/db"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("APP_DB_DSN")
	if dsn == "" {
		t.Skip("APP_DB_DSN not set; skipping integration test (see README.md)")
	}
	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := sqlDB.PingContext(context.Background()); err != nil {
		t.Skipf("cannot reach test database: %v", err)
	}
	return sqlDB
}

// TestRecordWithNilDetailRoundTrips guards against the exact NULL-Scan bug
// already hit twice in this codebase (submission.validation_detail,
// import_batch.error_detail): a nullable JSON column must always be written
// with a valid JSON value (the "null" literal when there's nothing to
// report), never a SQL NULL, or a later read into *json.RawMessage fails.
func TestRecordWithNilDetailRoundTrips(t *testing.T) {
	sqlDB := openTestDB(t)
	ctx := context.Background()
	queries := db.New(sqlDB)
	svc := audit.NewService(queries)

	runID := time.Now().UnixNano()
	entityType := fmt.Sprintf("audit-test-entity-%d", runID)
	entityID := uint64(runID % 1_000_000_007)

	if err := audit.Record(ctx, queries, audit.Params{
		Action: "test-action", EntityType: entityType, EntityID: &entityID, Detail: nil,
	}); err != nil {
		t.Fatalf("record with nil detail: %v", err)
	}

	rows, err := svc.List(ctx, audit.ListFilter{EntityType: entityType, Limit: 10})
	if err != nil {
		t.Fatalf("list audit logs (this is the NULL-Scan bug if it fails): %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected exactly 1 row for entity_type=%s, got %d", entityType, len(rows))
	}
	if string(rows[0].Detail) != "null" {
		t.Fatalf("detail = %q, want the JSON literal \"null\"", rows[0].Detail)
	}
}
