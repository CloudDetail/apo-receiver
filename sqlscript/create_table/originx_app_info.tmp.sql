CREATE TABLE IF NOT EXISTS originx_app_info{{if .Cluster}}_local ON CLUSTER {{.Cluster}}{{end}}
(
  timestamp DateTime CODEC(Delta, ZSTD(1)),
  start_time UInt64,
  heart_time UInt64,
  heart_flag UInt32,
  agent_instance_id LowCardinality(String) CODEC(ZSTD(1)),
  host_pid UInt32,
  container_pid UInt32,
  container_id String CODEC(ZSTD(1)),
  labels Map(LowCardinality(String), String) CODEC(ZSTD(1))
) ENGINE {{if .Replication}}ReplicatedMergeTree{{else}}MergeTree(){{end}}
    PARTITION BY toDate(timestamp)
    ORDER BY toUnixTimestamp(timestamp)
    SETTINGS index_granularity=8192