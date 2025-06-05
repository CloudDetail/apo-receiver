package tables

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/CloudDetail/apo-module/model/v1"
)

const (
	insertAgentEventSQL = `INSERT INTO "%s".originx_agent_event (
		timestamp,
		name,
		pid,
		labels,
		status
	) VALUES (
		?,
		?,
		?,
		?,
		?
	)`
)

func WriteAgentEvents(ctx context.Context, database string, conn *sql.DB, toSends []*model.AgentEvent) error {
	if len(toSends) == 0 {
		return nil
	}
	err := doWithTx(ctx, conn, func(tx *sql.Tx) error {
		statement, find := statementCache.GetStatement(database, "originx_agent_event")
		if !find {
			statement, err := tx.PrepareContext(ctx, fmt.Sprintf(insertAgentEventSQL, database))
			if err != nil {
				return fmt.Errorf("PrepareContext:%w", err)
			}
			statementCache.SetStatement(database, "originx_agent_event", statement)
		}
		var err error

		for _, toSend := range toSends {
			_, err = statement.ExecContext(ctx,
				time.Unix(int64(toSend.Timestamp), 0).UTC(),
				toSend.Name,
				toSend.Pid,
				toSend.Labels,
				toSend.Status,
			)
			if err != nil {
				return fmt.Errorf("ExecContext:%w", err)
			}
		}

		return nil
	})
	return err
}
