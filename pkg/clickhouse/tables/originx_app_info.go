package tables

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/CloudDetail/apo-receiver/pkg/analyzer/appinfo"
	"github.com/CloudDetail/apo-receiver/pkg/model"
)

const (
	insertServiceInstanceSQL = `INSERT INTO originx_app_info (
		timestamp,
		start_time,
		agent_instance_id,
		host_pid,
		container_pid,
		container_id,
		labels
	) VALUES (
		?,
		?,
		?,
		?,
		?,
		?,
		?
	)`

	queryStoredAppsSQL = `SELECT start_time, host_pid, labels['node_ip'] as node_ip, labels['node_name'] as node_name, labels['cluster_id'] as cluster_id, sum(case when labels['service_name'] != '' then 1 else 0 end) > 0 as related
		FROM originx_app_info
		WHERE timestamp >= ?
		GROUP BY node_ip, node_name, cluster_id, start_time, host_pid
	`

	queryYesterdayRelatedAppsSQL = `SELECT start_time, agent_instance_id, host_pid, container_pid, container_id, labels
		FROM originx_app_info
		WHERE timestamp BETWEEN ? AND ? AND labels['service_name'] != ''
	`

	queryRelatedAppsSQL = `SELECT start_time, host_pid, container_id,
		labels['pod_name'] as pod_name,
		labels['cluster_id'] as cluster_id,
		labels['source'] as source,
		labels['service_id'] as service_id,
		labels['service_name'] as service_name,
		labels['node_ip'] as node_ip,
		labels['node_name'] as node_name
		FROM originx_app_info
		WHERE timestamp >= ? AND service_name != ''
		ORDER BY node_ip, node_name, cluster_id, start_time, host_pid, timestamp desc
	`
)

func WriteAppInfos(ctx context.Context, conn *sql.DB, toSends []*appinfo.AppInfo) error {
	if len(toSends) == 0 {
		return nil
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	storedApps, err := queryStoredApps(ctx, conn, today)
	if err != nil {
		return err
	}

	newApps := make([]*appinfo.AppInfo, 0)
	for _, appInfo := range toSends {
		key := fmt.Sprintf("%s-%s-%s-%d-%d", appInfo.Labels["node_ip"], appInfo.Labels["node_name"], appInfo.Labels["cluster_id"], appInfo.HostPid, appInfo.StartTime)
		if _, ok := storedApps[key]; !ok {
			newApps = append(newApps, appInfo)
		}
	}
	if len(newApps) == 0 {
		return nil
	}

	relatedApps, err := queryYesterdayRelatedApps(ctx, conn, today)
	if err != nil {
		return err
	}

	newRelateApps := make([]*appinfo.AppInfo, 0)
	for _, appInfo := range newApps {
		key := fmt.Sprintf("%s-%s-%s-%d-%d", appInfo.Labels["node_ip"], appInfo.Labels["node_name"], appInfo.Labels["cluster_id"], appInfo.HostPid, appInfo.StartTime)
		if relateApp, ok := relatedApps[key]; ok {
			newRelateApps = append(newRelateApps, relateApp)
		}
	}


	err = doWithTx(ctx, conn, func(tx *sql.Tx) error {
		statement, err := tx.PrepareContext(ctx, insertServiceInstanceSQL)
		if err != nil {
			return fmt.Errorf("PrepareContext:%w", err)
		}
		defer func() {
			_ = statement.Close()
		}()
		for _, newRelate := range newRelateApps {
			_, err = statement.ExecContext(ctx,
				now.UTC(),
				newRelate.StartTime,
				newRelate.AgentInstance,
				newRelate.HostPid,
				newRelate.ContainerPid,
				newRelate.ContainerId,
				newRelate.Labels,
			)
			if err != nil {
				return fmt.Errorf("ExecContext:%w", err)
			}
			log.Printf("[Store Related App] %v", newRelate)
		}
		for _, newApp := range newApps {
			_, err = statement.ExecContext(ctx,
				now.UTC(),
				newApp.StartTime,
				newApp.AgentInstance,
				newApp.HostPid,
				newApp.ContainerPid,
				newApp.ContainerId,
				newApp.Labels,
			)
			if err != nil {
				return fmt.Errorf("ExecContext:%w", err)
			}
			log.Printf("[Store New App] %v", newApp)
		}
		return nil
	})
	return err
}

func queryStoredApps(ctx context.Context, conn *sql.DB, startTime int64) (map[string]bool, error) {
	rows, err := conn.Query(queryStoredAppsSQL, startTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]bool, 0)
	for rows.Next() {
		groupApp := &GroupApp{}
		if err = rows.Scan(
			&groupApp.StartTime,
			&groupApp.HostPid,
			&groupApp.NodeIp,
			&groupApp.NodeName,
			&groupApp.ClusterId,
			&groupApp.IsRelated); err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%s-%s-%s-%d-%d", groupApp.NodeIp, groupApp.NodeName, groupApp.ClusterId, groupApp.HostPid, groupApp.StartTime)
		result[key] = groupApp.IsRelated
	}
	return result, nil
}


func queryYesterdayRelatedApps(ctx context.Context, conn *sql.DB, endTime int64) (map[string]*appinfo.AppInfo, error) {
	rows, err := conn.Query(queryYesterdayRelatedAppsSQL, endTime - 24 * 3600, endTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]*appinfo.AppInfo, 0)
	for rows.Next() {
		appInfo := &appinfo.AppInfo{}
		if err = rows.Scan(
			&appInfo.StartTime,
			&appInfo.AgentInstance,
			&appInfo.HostPid,
			&appInfo.ContainerPid,
			&appInfo.ContainerId,
			&appInfo.Labels); err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%s-%s-%s-%d-%d", appInfo.Labels["node_ip"], appInfo.Labels["node_name"], appInfo.Labels["cluster_id"], appInfo.HostPid, appInfo.StartTime)
		result[key] = appInfo
	}
	return result, nil
}

func QueryRelatedAppInfos(ctx context.Context, conn *sql.DB) (map[string][]*model.QueryMonitedAppData, error) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	rows, err := conn.Query(queryRelatedAppsSQL, today - 24 * 3600)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string][]*model.QueryMonitedAppData, 0)
	for rows.Next() {
		relatedApp := &RelatedApp{}
		if err = rows.Scan(
			&relatedApp.StartTime,
			&relatedApp.HostPid,
			&relatedApp.ContainerId,
			&relatedApp.PodName,
			&relatedApp.ClusterId,
			&relatedApp.Source,
			&relatedApp.ServiceId,
			&relatedApp.ServiceName,
			&relatedApp.NodeIp,
			&relatedApp.NodeName); err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%s-%s-%s", relatedApp.NodeIp, relatedApp.NodeName, relatedApp.ClusterId)
		value, ok := result[key]
		if !ok {
			value = make([]*model.QueryMonitedAppData, 0)
		} else {
			last_value := value[len(value) - 1]
			// Clean Repeated Data
			if last_value.StartTime == relatedApp.StartTime && last_value.HostPid == relatedApp.HostPid {
				continue
			}
		}
		value = append(value, &model.QueryMonitedAppData{
			Source:      relatedApp.Source,
			ServiceId:   relatedApp.ServiceId,
			ServiceName: relatedApp.ServiceName,
			StartTime:   relatedApp.StartTime,
			HostPid:     relatedApp.HostPid,
			ContainerId: relatedApp.ContainerId,
			PodName:     relatedApp.PodName,
		})
		result[key] = value
	}
	return result, nil
}

type RelatedApp struct {
	StartTime   uint64 `db:"start_time"`
	HostPid     uint32 `db:"host_pid"`
	ContainerId string `db:"container_id"`
	PodName     string `db:"pod_name"`
	ClusterId   string `db:"cluster_id"`
	Source      string `db:"source"`
	ServiceId   string `db:"service_id"`
	ServiceName string `db:"service_name"`
	NodeIp      string `db:"node_ip"`
	NodeName    string `db:"node_name"`
}

type GroupApp struct {
	StartTime   uint64 `db:"start_time"`
	HostPid     uint32 `db:"host_pid"`
	NodeIp      string `db:"node_ip"`
	NodeName    string `db:"node_name"`
	ClusterId   string `db:"cluster_id"`
	IsRelated   bool   `db:"related"`
}
