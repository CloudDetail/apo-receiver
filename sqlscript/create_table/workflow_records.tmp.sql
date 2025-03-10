CREATE TABLE IF NOT EXISTS workflow_records{{if .Cluster}}_local ON CLUSTER {{.Cluster}}{{end}}
(
    `workflow_run_id` String,
    `workflow_id` String CODEC(ZSTD(1)),
    `workflow_name` String CODEC(ZSTD(1)),
    `ref` String,
    `input` String,
    `output` String,
    `created_at` DateTime64(3)
) ENGINE {{if .Replication}}ReplicatedMergeTree{{else}}MergeTree(){{end}}
PARTITION BY toDate(created_at)
ORDER BY (workflow_run_id,created_at)
TTL toDateTime(created_at) + toIntervalDay({{.TTLDay}})
SETTINGS index_granularity=8192, ttl_only_drop_parts = 1