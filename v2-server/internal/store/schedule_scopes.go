package store

import (
	"context"
	"errors"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
)

// ScheduleScopes discovers persisted owners without depending on browser sessions: every creator
// of a configuration who still has access to its tenant (TenantAccess). Paused configurations are
// included so already accepted work can still settle.
func (s *Store) ScheduleScopes(ctx context.Context) ([]execution.Scope, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT tenant_id,user_id FROM v2_schedule_configs ORDER BY tenant_id,user_id`)
	if err != nil {
		return nil, err
	}
	creators := []execution.Scope{}
	for rows.Next() {
		var item execution.Scope
		if err := rows.Scan(&item.TenantID, &item.UserID); err != nil {
			rows.Close()
			return nil, err
		}
		creators = append(creators, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	items := []execution.Scope{}
	for _, item := range creators {
		if _, err := s.TenantAccess(ctx, item.TenantID, item.UserID); errors.Is(err, ErrNotFound) {
			continue
		} else if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// GetScheduleConfig reads one configuration owned by its creator.
func (s *Store) GetScheduleConfig(ctx context.Context, tenant, user, id string) (ScheduleConfig, error) {
	return readSchedule(s.pool.QueryRow(ctx, `SELECT `+scheduleColumns+` FROM v2_schedule_configs WHERE tenant_id=$1 AND user_id=$2 AND id=$3`, tenant, user, id))
}
