package tables

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/CloudDetail/apo-receiver/pkg/analyzer/report"
)

const (
	insertServiceRelationShipSQL = `INSERT INTO "%s".service_relationship (
		timestamp,
		entry_service,
		entry_url,
		miss_top,
		trace_id,
		parent_service,
		parent_url,
		service,
		url,
		path,
		labels,
		flags
	) VALUES (
		?,
		?,
		?,
		?,
		?,
		?,
		?,
		?,
		?,
		?,
		?,
		?
	)`
)

func WriteServiceRelationships(ctx context.Context, database string, conn *sql.DB, toSends []*report.Relation) error {
	if len(toSends) == 0 {
		return nil
	}
	err := doWithTx(ctx, conn, func(tx *sql.Tx) error {
		statement, find := statementCache.GetStatement(database, "service_relationship")
		if !find {
			var err error
			statement, err = tx.PrepareContext(ctx, fmt.Sprintf(insertServiceRelationShipSQL, database))
			if err != nil {
				return fmt.Errorf("PrepareContext:%w", err)
			}
			statementCache.SetStatement(database, "service_relationship", statement)
		}
		var err error

		for _, toSend := range toSends {
			toSend.CollectRelationships()
			timestamp := asTime(int64(toSend.RootNode.StartTime))
			for _, relationship := range toSend.Relationships {
				labels := map[string]string{
					"client_group": relationship.ClientGroup,
					"client_type":  relationship.ClientType,
					"client_peer":  relationship.ClientPeer,
					"client_key":   relationship.ClientKey,
				}
				flags := map[string]bool{
					"parent_traced": relationship.ParentTraced,
					"is_async":      relationship.IsAsync,
					"is_traced":     relationship.IsTraced,
				}

				_, err = statement.ExecContext(ctx,
					timestamp,
					toSend.RootNode.ServiceName,
					toSend.RootNode.Url,
					!toSend.RootNode.TopNode, // miss_top
					toSend.TraceId,
					relationship.ParentService,
					relationship.ParentUrl,
					relationship.Service,
					relationship.Url,
					relationship.Path,
					labels,
					flags,
				)
			}
			if err != nil {
				return fmt.Errorf("ExecContext:%w", err)
			}
		}

		return nil
	})
	return err
}
