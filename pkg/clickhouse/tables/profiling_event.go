package tables

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
)

const (
	insertProfilingEventSQL = `INSERT INTO "%s".profiling_event (
		timestamp,
		data_version,
		pid,
		tid,
		startTime,
		endTime,
		cpuEvents,
		innerCalls,
		javaFutexEvents,
		spans,
		transactionIds,
		labels,
		offset_ts
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

type CameraEventGroup struct {
	Name        string              `json:"name"`
	Timestamp   uint64              `json:"timestamp"`
	DataVersion string              `json:"data_version"`
	Labels      *CameraProfileEvent `json:"labels"`
}

type CameraProfileEvent struct {
	CpuEvents       string `json:"cpuEvents"`
	ContainerId     string `json:"container_id"`
	NodeName        string `json:"node_name"`
	NodeIp          string `json:"node_ip"`
	EndTime         uint64 `json:"endTime"`
	InnerCalls      string `json:"innerCalls"`
	JavaFutexEvents string `json:"javaFutexEvents"`
	Pid             uint32 `json:"pid"`
	Protocol        string `json:"protocol"`
	Spans           string `json:"spans"`
	StartTime       uint64 `json:"startTime"`
	ThreadName      string `json:"threadName"`
	Tid             uint32 `json:"tid"`
	TransactionIds  string `json:"transactionIds"`
	OffsetTs        int64  `json:"offset_ts"`
}

func WriteProfilingEvents(ctx context.Context, database string, conn *sql.DB, toSends []string) error {
	if len(toSends) == 0 {
		return nil
	}

	err := doWithTx(ctx, conn, func(tx *sql.Tx) error {
		statement, find := statementCache.GetStatement(database, "profiling_event")
		if !find {
			statement, err := tx.PrepareContext(ctx, fmt.Sprintf(insertProfilingEventSQL, database))
			if err != nil {
				return fmt.Errorf("PrepareContext:%w", err)
			}
			statementCache.SetStatement(database, "profiling_event", statement)
		}

		var err error
		for _, eventGroupJson := range toSends {
			eventGroup := &CameraEventGroup{}
			if err := json.Unmarshal([]byte(eventGroupJson), eventGroup); err != nil {
				log.Printf("[x Parse Profile Event] Error: %s", err.Error())
				continue
			}
			labels := map[string]string{
				"container_id": eventGroup.Labels.ContainerId,
				"node_name":    eventGroup.Labels.NodeName,
				"node_ip":      eventGroup.Labels.NodeIp,
				"protocol":     eventGroup.Labels.Protocol,
				"threadName":   eventGroup.Labels.ThreadName,
			}
			_, err = statement.ExecContext(ctx,
				asTime(int64(eventGroup.Timestamp)), // Second
				eventGroup.DataVersion,
				eventGroup.Labels.Pid,
				eventGroup.Labels.Tid,
				eventGroup.Labels.StartTime,
				eventGroup.Labels.EndTime,
				eventGroup.Labels.CpuEvents,
				eventGroup.Labels.InnerCalls,
				eventGroup.Labels.JavaFutexEvents,
				eventGroup.Labels.Spans,
				eventGroup.Labels.TransactionIds,
				labels,
				eventGroup.Labels.OffsetTs,
			)
			if err != nil {
				return fmt.Errorf("ExecContext:%w", err)
			}
		}
		return nil
	})
	return err
}
