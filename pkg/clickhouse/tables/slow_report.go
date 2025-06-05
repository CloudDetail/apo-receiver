package tables

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/CloudDetail/apo-receiver/pkg/analyzer/report"
)

const (
	insertSlowReportSQL = `INSERT INTO "%s".slow_report (
		timestamp,
		is_drop,
		trace_id,
		duration,
		end_time,
		drop_reason,
		cause,
		relation_trees,
		otel_client_calls,
		labels,
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
        ?,
		?
	)`
)

func WriteSlowReports(ctx context.Context, database string, conn *sql.DB, toSends []*report.NodeReport) error {
	if len(toSends) == 0 {
		return nil
	}

	err := doWithTx(ctx, conn, func(tx *sql.Tx) error {
		statement, find := statementCache.GetStatement(database, "slow_report")
		if !find {
			var err error
			statement, err = tx.PrepareContext(ctx, fmt.Sprintf(insertSlowReportSQL, database))
			if err != nil {
				return fmt.Errorf("PrepareContext:%w", err)
			}
			statementCache.SetStatement(database, "slow_report", statement)
		}

		var err error

		for _, nodeReport := range toSends {
			relationTrees := ""
			if nodeReport.Data.RelationTree != nil {
				relationTreesByte, _ := json.Marshal(nodeReport.Data.RelationTree)
				relationTrees = string(relationTreesByte)
			}
			clientCalls := ""
			if nodeReport.Data.OTelClientCalls != nil {
				clientCallsByte, _ := json.Marshal(nodeReport.Data.OTelClientCalls)
				clientCalls = string(clientCallsByte)
			}

			labels := map[string]string{
				"entry_service":         nodeReport.Data.EntryService,
				"entry_instance":        nodeReport.Data.EntryInstance,
				"mutated_service":       nodeReport.Data.MutatedService,
				"mutated_url":           nodeReport.Data.MutatedUrl,
				"mutated_instance":      nodeReport.Data.MutatedInstance,
				"span_id":               nodeReport.Data.MutatedSpan,
				"mutated_pod":           nodeReport.Data.MutatedPod,
				"mutated_pod_ns":        nodeReport.Data.MutatedPodNS,
				"mutated_workload_name": nodeReport.Data.MutatedWorkloadName,
				"mutated_workload_type": nodeReport.Data.MutatedWorkloadType,
				"content_key":           nodeReport.Data.ContentKey,
			}
			_, err = statement.ExecContext(ctx,
				asTime(int64(nodeReport.Timestamp)), // NanoTime
				nodeReport.IsDrop,
				nodeReport.TraceId,
				nodeReport.Duration,
				nodeReport.Data.EndTime,
				nodeReport.Data.DropReason,
				nodeReport.Data.Cause,
				relationTrees,
				clientCalls,
				labels,
				string(nodeReport.Data.ThresholdType),
				string(nodeReport.Data.ThresholdRange),
				nodeReport.Data.ThresholdValue,
				nodeReport.Data.ThresholdMultiple,
			)
			if err != nil {
				return fmt.Errorf("ExecContext:%w", err)
			}
		}
		return nil
	})
	return err
}
