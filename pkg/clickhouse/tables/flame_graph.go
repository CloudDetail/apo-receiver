package tables

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
)

const (
	insertFlameGraphSQL = `INSERT INTO "%s".flame_graph (
		start_time,
		end_time,
		pid,
		tid,
		sample_type,
		sample_rate,
		labels,
		flamebearer
	) VALUES (
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

type FlameGraphEvent struct {
	Pid         uint32 `json:"pid"`
	Tid         uint32 `json:"tid"`
	ContainerId string `json:"container_id"`
	NsPid       int    `json:"ns_pid"`
	NodeName    string `json:"node_name"`
	NodeIp      string `json:"node_ip"`
	SampleType  string `json:"sample_type"`
	SampleRate  uint32 `json:"sample_rate"`
	TraceId     string `json:"trace_id"`
	SpanId      string `json:"span_id"`
	Flamebearer string `json:"flamebearer"`
	StartTime   uint64 `json:"start_time"`
	EndTime     uint64 `json:"end_time"`
}

func WriteFlameGraph(ctx context.Context, database string, conn *sql.DB, toSends []string) error {
	if len(toSends) == 0 {
		return nil
	}

	err := doWithTx(ctx, conn, func(tx *sql.Tx) error {
		statement, find := statementCache.GetStatement(database, "flame_graph")
		if !find {
			statement, err := tx.PrepareContext(ctx, fmt.Sprintf(insertFlameGraphSQL, database))
			if err != nil {
				return fmt.Errorf("PrepareContext:%w", err)
			}
			statementCache.SetStatement(database, "flame_graph", statement)
		}

		var err error

		for _, flameGraphJson := range toSends {
			flameGraphEvent := &FlameGraphEvent{}
			if err := json.Unmarshal([]byte(flameGraphJson), flameGraphEvent); err != nil {
				log.Printf("[x Parse Flame Graph] Error: %s", err.Error())
				continue
			}
			labels := map[string]string{
				"node_name":    flameGraphEvent.NodeName,
				"node_ip":      flameGraphEvent.NodeIp,
				"container_id": flameGraphEvent.ContainerId,
				"ns_pid":       strconv.Itoa(flameGraphEvent.NsPid),
			}
			if len(flameGraphEvent.TraceId) > 0 {
				labels["trace_id"] = flameGraphEvent.TraceId
			}
			if len(flameGraphEvent.SpanId) > 0 {
				labels["span_id"] = flameGraphEvent.SpanId
			}
			if _, err = statement.ExecContext(ctx,
				asTime(int64(flameGraphEvent.StartTime)),
				asTime(int64(flameGraphEvent.EndTime)),
				flameGraphEvent.Pid,
				flameGraphEvent.Tid,
				flameGraphEvent.SampleType,
				flameGraphEvent.SampleRate,
				labels,
				flameGraphEvent.Flamebearer); err != nil {

				return fmt.Errorf("ExecContext:%w", err)
			}
		}
		return nil
	})
	return err
}
