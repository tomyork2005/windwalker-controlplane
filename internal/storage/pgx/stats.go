package pgx

import (
	"context"
	"control-plane/internal/model"
	"fmt"
	"strings"
	"time"
)

func (s *Storage) InsertUserTrafficBatch(
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
	b.WriteString("INSERT INTO user_traffic (agent_id, user_id, window_end, bytes_up, bytes_down, ip_count) VALUES ")
	for i, r := range rows {
		if i > 0 {
			b.WriteString(", ")
		}
		base := len(args) + 1
		fmt.Fprintf(&b, "($1, $%d, $2, $%d, $%d, $%d)", base, base+1, base+2, base+3)
		args = append(args, r.UserID, int64(r.BytesUp), int64(r.BytesDown), int32(r.IPCount))
	}

	if _, err := s.getExecutor(ctx).Exec(ctx, b.String(), args...); err != nil {
		return fmt.Errorf("insert user_traffic batch: %w", err)
	}
	return nil
}
