package tables

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
)

const (
	insertJvmGcSQL = `INSERT INTO "%s".jvm_gc (
		timestamp,
		pid,
		labels,
		ygc,
		fgc,
		last_ygc,
		last_fgc,
		ygc_last_entry_time,
		fgc_last_entry_time,
		ygc_span,
		fgc_span
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
		?
	)`
)

type JvmGcInfo struct {
	Pid              string `json:"pid"`
	NodeName         string `json:"node_name"`
	NodeIp           string `json:"node_ip"`
	Ygc              int64  `json:"ygc"`
	Fgc              int64  `json:"fgc"`
	LastYgc          int64  `json:"last_ygc"`
	LastFgc          int64  `json:"last_fgc"`
	YgcLastEntryTime int64  `json:"ygc_last_entry_time"`
	FgcLastEntryTime int64  `json:"fgc_last_entry_time"`
	YgcSpan          int64  `json:"ygc_span"`
	FgcSpan          int64  `json:"fgc_span"`
	Timestamp        uint64 `json:"timestamp"`
}

func WriteJvmGcs(ctx context.Context, database string, conn *sql.DB, toSends []string) error {
	if len(toSends) == 0 {
		return nil
	}

	err := doWithTx(ctx, conn, func(tx *sql.Tx) error {
		statement, find := statementCache.GetStatement(database, "jvm_gc")
		if !find {
			var err error
			statement, err = tx.PrepareContext(ctx, fmt.Sprintf(insertJvmGcSQL, database))
			if err != nil {
				return fmt.Errorf("PrepareContext:%w", err)
			}
			statementCache.SetStatement(database, "jvm_gc", statement)
		}

		var err error
		for _, jvmGcJson := range toSends {
			jvmGc := &JvmGcInfo{}
			if err := json.Unmarshal([]byte(jvmGcJson), jvmGc); err != nil {
				log.Printf("[x Parse Jvm Gc] Error: %s", err.Error())
				continue
			}
			labels := map[string]string{
				"node_name": jvmGc.NodeName,
				"node_ip":   jvmGc.NodeIp,
			}
			_, err = statement.ExecContext(ctx,
				asTime(int64(jvmGc.Timestamp)), // NanoTime
				jvmGc.Pid,
				labels,
				jvmGc.Ygc,
				jvmGc.Fgc,
				jvmGc.LastYgc,
				jvmGc.LastFgc,
				jvmGc.YgcLastEntryTime,
				jvmGc.FgcLastEntryTime,
				jvmGc.YgcSpan,
				jvmGc.FgcSpan,
			)
			if err != nil {
				return fmt.Errorf("ExecContext:%w", err)
			}
		}
		return nil
	})
	return err
}
