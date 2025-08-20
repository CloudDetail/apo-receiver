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
		heart_time,
		heart_flag,
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
		?,
		?,
		?
	)`

	queryStoredAppCountSQL = `SELECT count(1) FROM originx_app_info
		WHERE start_time = %d AND host_pid = %d AND labels['node_ip'] = '%s' AND labels['node_name'] = '%s'
	`

	updateHeartTimeSQL = "ALTER TABLE originx_app_info UPDATE heart_time=%d, heart_flag=0 WHERE start_time=%d AND host_pid=%d AND labels['node_ip'] = '%s' AND labels['node_name'] = '%s'"

	updateHeartDeadSQL = "ALTER TABLE originx_app_info UPDATE heart_flag=%d WHERE start_time=%d AND host_pid=%d AND labels['node_ip'] = '%s' AND labels['node_name'] = '%s'"

	queryActiveAppsSQL = `SELECT host_pid, start_time
		FROM originx_app_info
		WHERE heart_flag < 2 AND labels['node_ip'] = '%s' AND labels['node_name'] = '%s'
		ORDER BY host_pid, start_time
	`
)

const (
	HEART_FLAG_ALIVE = 0
	HEART_FLAG_MISS  = 1
	HEART_FLAG_DEAD  = 2
)

func WriteAppInfos(ctx context.Context, conn *sql.DB, toSends []*appinfo.AppInfo) error {
	if len(toSends) == 0 {
		return nil
	}

	return doWithTx(ctx, conn, func(tx *sql.Tx) error {
		statement, err := tx.PrepareContext(ctx, insertServiceInstanceSQL)
		if err != nil {
			return fmt.Errorf("PrepareContext:%w", err)
		}
		defer func() {
			_ = statement.Close()
		}()

		now := time.Now()
		for _, toSend := range toSends {
			var count int
			if err := conn.QueryRow(fmt.Sprintf(queryStoredAppCountSQL, toSend.StartTime, toSend.HostPid, toSend.Labels["node_ip"], toSend.Labels["node_name"])).Scan(&count); err != nil {
				return fmt.Errorf("fail to query app count: s%w", err)
			}
			if count == 0 {
				if _, err = statement.ExecContext(ctx,
					now.UTC(),
					toSend.StartTime,
					now.Unix(), // Second
					HEART_FLAG_ALIVE,
					toSend.AgentInstance,
					toSend.HostPid,
					toSend.ContainerPid,
					toSend.ContainerId,
					toSend.Labels,
				); err != nil {
					return fmt.Errorf("ExecContext:%w", err)
				}
				log.Printf("[Store New App] %v", toSend)
			} else {
				log.Printf("[Ignore Exist App] %v", toSend)
			}
		}
		return nil
	})
}

func QueryActiveApps(ctx context.Context, conn *sql.DB, nodeIp string, nodeName string, deadApps []*model.QueryActiveApp) ([]*model.QueryActiveApp, error) {
	rows, err := conn.Query(fmt.Sprintf(queryActiveAppsSQL, nodeIp, nodeName))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	deadKeys := map[string]struct{}{}
	for _, deadApp := range deadApps {
		deadKeys[fmt.Sprintf("%d-%d", deadApp.Pid, deadApp.StartTime)] = struct{}{}
	}
	result := make([]*model.QueryActiveApp, 0)
	for rows.Next() {
		activeApp := &ActiveApp{}
		if err = rows.Scan(
			&activeApp.HostPid,
			&activeApp.StartTime); err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%d-%d", activeApp.HostPid, activeApp.StartTime)
		if _, exist := deadKeys[key]; !exist {
			result = append(result, &model.QueryActiveApp{
				Pid:       activeApp.HostPid,
				StartTime: activeApp.StartTime,
			})
		}
	}
	return result, nil
}

func UpdateAppHeartTimes(conn *sql.DB, nodeIp string, nodeName string, activeApps []*model.QueryActiveApp) error {
	heartTime := time.Now().Unix()
	log.Printf("[Update App HeartTime] NodeIp: %s, Count: %d", nodeIp, len(activeApps))
	for _, activeApp := range activeApps {
		if _, err := conn.Exec(fmt.Sprintf(updateHeartTimeSQL, heartTime, activeApp.StartTime, activeApp.Pid, nodeIp, nodeName)); err != nil {
			return err
		}
	}
	return nil
}

func UpdateAppDeadFlags(conn *sql.DB, nodeIp string, nodeName string, deadApps []*model.QueryActiveApp) error {
	for _, deadApp := range deadApps {
		log.Printf("[Update App DeadFlag] NodeIp: %s, Pid: %d", nodeIp, deadApp.Pid)
		if _, err := conn.Exec(fmt.Sprintf(updateHeartDeadSQL, HEART_FLAG_DEAD, deadApp.StartTime, deadApp.Pid, nodeIp, nodeName)); err != nil {
			return err
		}
	}
	return nil
}

type ActiveApp struct {
	HostPid   uint32 `db:"host_pid"`
	StartTime uint64 `db:"start_time"`
}
