package tables

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/CloudDetail/apo-receiver/pkg/analyzer/report"
)

const (
	insertErrorReportSQL = `INSERT INTO "%s".error_report (
		timestamp,
		is_drop,
		trace_id,
		duration,
		end_time,
		drop_reason,
		labels,
		cause,
		cause_message,
		relation_trees,
		threshold_type,
		threshold_range,
		threshold_value,
		threshold_multiple
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
		?
	)`
)

func WriteErrorReports(ctx context.Context, database string, conn *sql.DB, toSends []*report.ErrorReport) error {
	if len(toSends) == 0 {
		return nil
	}

	err := doWithTx(ctx, conn, func(tx *sql.Tx) error {
		statement, err := tx.PrepareContext(ctx, fmt.Sprintf(insertErrorReportSQL, database))
		if err != nil {
			return fmt.Errorf("PrepareContext:%w", err)
		}
		defer statement.Close()

		for _, errorReport := range toSends {
			relationTrees := ""
			if errorReport.Data.RelationTree != nil {
				relationTreesByte, _ := json.Marshal(errorReport.Data.RelationTree)
				relationTrees = string(relationTreesByte)
			}
			labels := map[string]string{
				"mutated_container_id":  errorReport.Data.MutatedContainerId,
				"entry_service":         errorReport.Data.EntryService,
				"entry_instance":        errorReport.Data.EntryInstance,
				"mutated_service":       errorReport.Data.MutatedService,
				"mutated_url":           errorReport.Data.MutatedUrl,
				"mutated_instance":      errorReport.Data.MutatedInstance,
				"span_id":               errorReport.Data.MutatedSpan,
				"mutated_pod":           errorReport.Data.MutatedPod,
				"mutated_pod_ns":        errorReport.Data.MutatedPodNS,
				"mutated_workload_name": errorReport.Data.MutatedWorkloadName,
				"mutated_workload_type": errorReport.Data.MutatedWorkloadType,
				"content_key":           errorReport.Data.ContentKey,
			}
			if _, err = statement.ExecContext(ctx,
				asTime(int64(errorReport.Timestamp)), // NanoTime
				errorReport.IsDrop,
				errorReport.TraceId,
				errorReport.Duration,
				errorReport.Data.EndTime,
				errorReport.Data.DropReason,
				labels,
				errorReport.Data.Cause,
				errorReport.Data.CauseMessage,
				relationTrees,
				string(errorReport.Data.ThresholdType),
				string(errorReport.Data.ThresholdRange),
				errorReport.Data.ThresholdValue,
				errorReport.Data.ThresholdMultiple); err != nil {

				return fmt.Errorf("ExecContext:%w", err)
			}
		}
		return nil
	})
	return err
}
