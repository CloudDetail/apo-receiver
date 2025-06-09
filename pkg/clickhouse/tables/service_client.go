package tables

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/CloudDetail/apo-receiver/pkg/analyzer/external"
	"github.com/CloudDetail/apo-receiver/pkg/analyzer/report"
	"github.com/CloudDetail/apo-receiver/pkg/metrics"
	metricModel "github.com/CloudDetail/apo-receiver/pkg/metrics/model"
)

const (
	insertServiceClientSQL = `INSERT INTO "%s".service_client (
		timestamp,
		entry_service,
		entry_url,
		miss_top,
		trace_id,
		client_span_id,
		client_service,
		client_url,
		next_span_id,
		client_time,
		client_duration,
		client_error,
		labels
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

	sourceAdapter = "adapter"
)

func WriteServiceClients(ctx context.Context, database string, conn *sql.DB, toSends []*report.Relation) error {
	if len(toSends) == 0 {
		return nil
	}

	err := doWithTx(ctx, conn, func(tx *sql.Tx) error {
		statement, err := tx.PrepareContext(ctx, fmt.Sprintf(insertServiceClientSQL, database))
		if err != nil {
			return fmt.Errorf("PrepareContext:%w", err)
		}
		defer statement.Close()

		for _, toSend := range toSends {
			timestamp := asTime(int64(toSend.RootNode.StartTime))
			for _, externalNode := range toSend.CollectExternalNodes() {
				for _, external := range externalNode.Externals {
					labels := map[string]string{
						"client_group":  external.Group,
						"client_type":   external.Type,
						"client_kind":   external.Kind.String(),
						"client_name":   external.Name,
						"client_peer":   external.Peer,
						"client_detail": external.Detail,
					}
					_, err = statement.ExecContext(ctx,
						timestamp,
						toSend.RootNode.ServiceName,
						toSend.RootNode.Url,
						!toSend.RootNode.TopNode, // miss_top
						toSend.TraceId,
						external.SpanId,
						externalNode.ServiceName,
						externalNode.Url,
						external.NextSpanId,
						external.StartTime,
						external.Duration,
						external.Error,
						labels,
					)
					if err != nil {
						return fmt.Errorf("ExecContext:%w", err)
					}
				}
			}
		}
		return nil
	})
	return err
}

func WriteClientMetric(toSends []*report.Relation, meitricWithUrl bool, accountID string) error {
	// TODO set account
	if len(toSends) == 0 {
		return nil
	}

	for _, toSend := range toSends {
		for _, externalNode := range toSend.CollectExternalNodes() {
			url := ""
			if meitricWithUrl {
				url = externalNode.Url
			}

			for _, externalRecord := range externalNode.Externals {
				if externalRecord.Group == external.GroupExternal {
					if err := metrics.UpdateMetric(accountID, metricModel.MetricExternalDuration, []string{
						externalNode.ServiceName,
						url,
						externalNode.NodeName,
						externalNode.NodeIp,
						strconv.Itoa(externalNode.Pid),
						externalNode.ContainerId,
						externalRecord.Name,
						externalRecord.Type,
						externalRecord.Peer,
						strconv.FormatBool(externalRecord.Error),
						sourceAdapter,
					}, float64(externalRecord.Duration)); err != nil {
						return err
					}
				} else if externalRecord.Group == external.GroupDb {
					if err := metrics.UpdateMetric(accountID, metricModel.MetricDbDuration, []string{
						externalNode.ServiceName,
						url,
						externalNode.NodeName,
						externalNode.NodeIp,
						strconv.Itoa(externalNode.Pid),
						externalNode.ContainerId,
						externalRecord.Name,
						externalRecord.Type,
						getDbName(externalRecord.Name),
						externalRecord.Peer,
						strconv.FormatBool(externalRecord.Error),
						sourceAdapter,
					}, float64(externalRecord.Duration)); err != nil {
						return err
					}
				} else if externalRecord.Group == external.GroupMq {
					if err := metrics.UpdateMetric(accountID, metricModel.MetricMqDuration, []string{
						externalNode.ServiceName,
						url,
						externalNode.NodeName,
						externalNode.NodeIp,
						strconv.Itoa(externalNode.Pid),
						externalNode.ContainerId,
						externalRecord.Name,
						externalRecord.Type,
						externalRecord.Kind.String(),
						externalRecord.Peer,
						strconv.FormatBool(externalRecord.Error),
						sourceAdapter,
					}, float64(externalRecord.Duration)); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func getDbName(name string) string {
	index := strings.Index(name, " ")
	if index < 4 {
		return ""
	}
	dbTable := name[index+1:]
	if index = strings.Index(dbTable, "."); index != -1 {
		return dbTable[0:index]
	}
	return ""
}
