package store

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// historyOwner is the creator-ownership rule: only the configuration's creator reads its history.
const historyOwner = `SELECT EXISTS(SELECT 1 FROM v2_schedule_configs WHERE id=$1 AND tenant_id=$2 AND user_id=$3)`

// ScheduleHistory returns a stable time-descending (recorded_at DESC, id DESC) page plus one
// lookahead row. Authorization and reads share a database snapshot. No status reconciliation
// or execution is performed.
func (s *Store) ScheduleHistory(ctx context.Context, tenant, user, scheduleID string, before OccurrenceCursor, limit int) ([]Occurrence, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrIneligible
	}
	cursor, ok := normalizeOccurrenceCursor(before)
	if !ok {
		return nil, ErrIneligible
	}
	out := []Occurrence{}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var allowed bool
		if err := tx.QueryRow(ctx, historyOwner, scheduleID, tenant, user).Scan(&allowed); err != nil {
			return err
		}
		if !allowed {
			return ErrNotFound
		}
		var (
			rows pgx.Rows
			err  error
		)
		if cursor.ID == "" {
			rows, err = tx.Query(ctx, `SELECT `+occurrenceColumns+` FROM v2_schedule_occurrences WHERE schedule_id=$1 ORDER BY recorded_at DESC,id DESC LIMIT $2`, scheduleID, limit+1)
		} else {
			rows, err = tx.Query(ctx, `SELECT `+occurrenceColumns+` FROM v2_schedule_occurrences WHERE schedule_id=$1 AND (recorded_at,id)<($2::timestamptz,$3::uuid) ORDER BY recorded_at DESC,id DESC LIMIT $4`, scheduleID, cursor.RecordedAt, cursor.ID, limit+1)
		}
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			o, err := scanOccurrence(rows)
			if err != nil {
				return err
			}
			out = append(out, o)
		}
		return rows.Err()
	})
	return out, err
}

// ScheduleHistoryItem hides absent, foreign and inaccessible records behind ErrNotFound.
func (s *Store) ScheduleHistoryItem(ctx context.Context, tenant, user, scheduleID, id string) (Occurrence, error) {
	return scanOccurrence(s.pool.QueryRow(ctx, `SELECT `+occurrenceColumns+` FROM v2_schedule_occurrences WHERE id=$4 AND schedule_id=$1 AND (`+historyOwner+`)`, scheduleID, tenant, user, id))
}
