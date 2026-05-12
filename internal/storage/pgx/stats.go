package pgx

import (
	"context"
	"control-plane/internal/model"
	"fmt"
	"strings"
	"time"
)

func (s *Storage) UpsertUserTrafficBatch(
	ctx context.Context,
	agentID string,
	windowEnd time.Time,
	rows []model.UserUsageRow,
) error {
	if len(rows) == 0 {
		return nil
	}

	args := make([]any, 0, 2+len(rows)*4)
	args = append(args, agentID, windowEnd)

	var b strings.Builder
	b.WriteString("INSERT INTO user_traffic (user_id, agent_id, bytes_up, bytes_down, last_ip_count, last_window_end, last_updated_at) VALUES ")
	for i, r := range rows {
		if i > 0 {
			b.WriteString(", ")
		}
		base := len(args) + 1
		fmt.Fprintf(&b, "($%d, $1, $%d, $%d, $%d, $2, now())", base, base+1, base+2, base+3)
		args = append(args, r.UserID, int64(r.BytesUp), int64(r.BytesDown), int32(r.IPCount))
	}
	b.WriteString(` ON CONFLICT (user_id) DO UPDATE SET
		agent_id        = EXCLUDED.agent_id,
		bytes_up        = user_traffic.bytes_up   + EXCLUDED.bytes_up,
		bytes_down      = user_traffic.bytes_down + EXCLUDED.bytes_down,
		last_ip_count   = EXCLUDED.last_ip_count,
		last_window_end = EXCLUDED.last_window_end,
		last_updated_at = now()
		WHERE EXCLUDED.last_window_end >= user_traffic.last_window_end`)

	if _, err := s.getExecutor(ctx).Exec(ctx, b.String(), args...); err != nil {
		return fmt.Errorf("upsert user_traffic batch: %w", err)
	}
	return nil
}
