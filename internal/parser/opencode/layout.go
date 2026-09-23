package opencode

import (
	"context"
	"database/sql"
	"fmt"
)

// SessionSource describes one supported, explicitly named OpenCode schema.
type SessionSource struct {
	Table, Messages, Parent, Filter string
	V2                              bool
}

func SessionSources(ctx context.Context, db *sql.DB) ([]SessionSource, error) {
	var sources []SessionSource
	var v2 int
	if err := db.QueryRowContext(ctx, `select count(*) from sqlite_master where type='table' and name='session_v2'`).Scan(&v2); err != nil {
		return nil, err
	}
	for _, source := range []SessionSource{{Table: "session_v2", Messages: "session_message", V2: true}, {Table: "session", Messages: "message"}} {
		var exists int
		if err := db.QueryRowContext(ctx, `select count(*) from sqlite_master where type='table' and name=?`, source.Table).Scan(&exists); err != nil {
			return nil, err
		}
		if exists == 0 {
			continue
		}
		var parent int
		if err := db.QueryRowContext(ctx, `select count(*) from pragma_table_info(?) where name='parent_id'`, source.Table).Scan(&parent); err != nil {
			return nil, err
		}
		source.Parent = "''"
		if parent > 0 {
			source.Parent = "coalesce(s.parent_id, '')"
		}
		if !source.V2 && v2 > 0 {
			source.Filter = " where not exists (select 1 from session_v2 v where v.id=s.id)"
		}
		sources = append(sources, source)
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("no supported OpenCode session tables")
	}
	return sources, nil
}
