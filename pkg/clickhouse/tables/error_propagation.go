package tables

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/CloudDetail/apo-receiver/pkg/analyzer/report"
)

const (
	insertErrorPropagationSQL = `INSERT INTO error_propagation (
		timestamp,
		entry_service,
		entry_url,
		entry_span_id,
		trace_id,
		nodes.service,
		nodes.instance,
		nodes.url,
		nodes.is_traced,
		nodes.is_error,
		nodes.error_types,
		nodes.error_msgs,
		nodes.depth,
		nodes.path
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
		?,
		?,
		?
	)`
)

func WriteErrorPropagations(ctx context.Context, conn *sql.DB, toSends []*report.ErrorReport) error {
	if len(toSends) == 0 {
		return nil
	}

	err := doWithTx(ctx, conn, func(tx *sql.Tx) error {
		statement, err := tx.PrepareContext(ctx, insertErrorPropagationSQL)
		if err != nil {
			return fmt.Errorf("PrepareContext:%w", err)
		}
		defer func() {
			_ = statement.Close()
		}()
		for _, errorReport := range toSends {
			if errorReport.IsDrop || errorReport.Data.RelationTree == nil {
				continue
			}

			rootNode := errorReport.Data.RelationTree
			errorPropagation := report.NewErrorPropagation(rootNode)
			if _, err = statement.ExecContext(ctx,
				asTime(int64(errorReport.Timestamp)), // NanoTime
				rootNode.ServiceName,
				rootNode.Url,
				rootNode.SpanId,
				errorReport.TraceId,
				errorPropagation.GetServiceList(),
				errorPropagation.GetInstanceList(),
				errorPropagation.GetUrlList(),
				errorPropagation.GetIsTracedList(),
				errorPropagation.GetIsErrorList(),
				errorPropagation.GetErrorTypeList(),
				errorPropagation.GetErrorMessageList(),
				errorPropagation.GetDepthList(),
				errorPropagation.GetPathList()); err != nil {

				return fmt.Errorf("ExecContext:%w", err)
			}
		}
		return nil
	})
	return err
}
