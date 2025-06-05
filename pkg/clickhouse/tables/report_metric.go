package tables

import (
	"context"
	"database/sql"
	"fmt"

	profile_model "github.com/CloudDetail/apo-receiver/pkg/componment/profile/model"
)

const (
	insertReportMetricSQL = `INSERT INTO "%s".report_metric (
		timestamp,
		entry_service,
		entry_url,
		mutated_service,
		mutated_url,
		total,
		success
	) VALUES (
		?,
        ?,
        ?,
        ?,
        ?,
        ?,
        ?
	)`
)

func WriteReportMetrics(ctx context.Context, database string, conn *sql.DB, toSends []*profile_model.SlowReportCountMetric) error {
	if len(toSends) == 0 {
		return nil
	}

	err := doWithTx(ctx, conn, func(tx *sql.Tx) error {
		statement, err := tx.PrepareContext(ctx, fmt.Sprintf(insertReportMetricSQL, database))
		if err != nil {
			return fmt.Errorf("PrepareContext:%w", err)
		}
		defer statement.Close()

		for _, reportMetric := range toSends {
			_, err = statement.ExecContext(ctx,
				asTime(reportMetric.Timestamp), // NanoTime
				reportMetric.EntryService,
				reportMetric.EntryService,
				reportMetric.MutatedService,
				reportMetric.MutatedUrl,
				reportMetric.Total,
				reportMetric.Success,
			)
			if err != nil {
				return fmt.Errorf("ExecContext:%w", err)
			}
		}
		return nil
	})
	return err
}
