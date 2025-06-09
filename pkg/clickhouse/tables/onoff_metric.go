package tables

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
)

const (
	insertOnoffMetricSQL = `INSERT INTO "%s".onoff_metric (
		timestamp,
		pid,
		tid,
		container_id,
		trace_id,
		apm_span_id,
		metrics
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

type OnOffMetric struct {
	Timestamp   uint64 `json:"timestamp"`
	Pid         uint32 `json:"pid"`
	Tid         uint32 `json:"tid"`
	ContainerId string `json:"container_id"`
	TraceId     string `json:"trace_id"`
	SpanId      string `json:"span_id"`
	Metrics     string `json:"metrics"`
}

func WriteOnOffMetrics(ctx context.Context, database string, conn *sql.DB, toSends []string) error {
	if len(toSends) == 0 {
		return nil
	}
	err := doWithTx(ctx, conn, func(tx *sql.Tx) error {
		statement, err := tx.PrepareContext(ctx, fmt.Sprintf(insertOnoffMetricSQL, database))
		if err != nil {
			return fmt.Errorf("PrepareContext:%w", err)
		}
		defer statement.Close()

		for _, onoffJson := range toSends {
			onOffMetric := &OnOffMetric{}
			if err := json.Unmarshal([]byte(onoffJson), onOffMetric); err != nil {
				log.Printf("[x Parse Onoff Metric] Error: %s", err.Error())
				continue
			}
			_, err = statement.ExecContext(ctx,
				asTime(int64(onOffMetric.Timestamp)), // NanoTime
				onOffMetric.Pid,
				onOffMetric.Tid,
				onOffMetric.ContainerId,
				onOffMetric.TraceId,
				onOffMetric.SpanId,
				onOffMetric.Metrics,
			)
			if err != nil {
				return fmt.Errorf("ExecContext:%w", err)
			}
		}
		return nil
	})
	return err
}
