package actual

import (
	"context"
	"database/sql"
)

// 割当の根拠（actual_entries.allocated_by）。
const (
	byActivityCode = "activity_code" // 箱の ID が施策コード
	byExternalCode = "external_code" // 箱の ID が外部コード
	byRule         = "rule"          // 割当ルール（会計科目 × 部門）
	byManual       = "manual"        // 未割当の一覧から選んだ
	byUnallocated  = "unallocated"   // 未割当
)

var allocationKinds = []string{byActivityCode, byExternalCode, byRule, byManual, byUnallocated}

type ruleKey struct {
	accountID int64
	dept      string // "" は全部門
}

// allocator は明細を施策に割り当てる（docs/plan.md「2.12」の割当の順番）。
type allocator struct {
	activities map[string]int64 // 施策コード → 施策
	externals  map[string]int64 // 外部コード → 施策
	rules      map[ruleKey]int64
}

// allocate は、箱の ID（施策コード → 外部コード）→ 部門まで一致する割当ルール → 部門が空のルール の順に施策を探す。
// どれにも当たらなければ未割当（nil）。
func (a *allocator) allocate(accountID int64, dept, box string) (*int64, string) {
	if box != "" {
		if id, ok := a.activities[box]; ok {
			return &id, byActivityCode
		}
		if id, ok := a.externals[box]; ok {
			return &id, byExternalCode
		}
	}
	if dept != "" {
		if id, ok := a.rules[ruleKey{accountID, dept}]; ok {
			return &id, byRule
		}
	}
	if id, ok := a.rules[ruleKey{accountID, ""}]; ok {
		return &id, byRule
	}
	return nil, byUnallocated
}

func loadAllocator(ctx context.Context, tx *sql.Tx) (*allocator, error) {
	a := &allocator{rules: map[ruleKey]int64{}}
	var err error
	if a.activities, err = codeIndex(ctx, tx, "SELECT code, id FROM activities"); err != nil {
		return nil, err
	}
	if a.externals, err = codeIndex(ctx, tx, "SELECT code, activity_id FROM activity_external_codes"); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT gl_account_id, department_key, activity_id FROM allocation_rules")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k ruleKey
		var id int64
		if err := rows.Scan(&k.accountID, &k.dept, &id); err != nil {
			return nil, err
		}
		a.rules[k] = id
	}
	return a, rows.Err()
}
