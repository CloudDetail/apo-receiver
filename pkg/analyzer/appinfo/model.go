package appinfo

type AppInfo struct {
	Timestamp     uint64            `json:"timestamp"`
	StartTime     uint64            `db:"start_time" json:"start_time"`
	HeartTime     uint64            `db:"heart_time" json:"heart_time"`
	HeartFlag     uint32            `db:"heart_flag" json:"heart_flag"`
	AgentInstance string            `db:"agent_instance_id" json:"agent_instance"`
	HostPid       uint32            `db:"host_pid" json:"host_pid"`
	ContainerPid  uint32            `db:"container_pid" json:"container_pid"`
	ContainerId   string            `db:"container_id" json:"container_id"`
	Labels        map[string]string `db:"labels" json:"labels"`
}
