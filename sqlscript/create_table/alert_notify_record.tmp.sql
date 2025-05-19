CREATE TABLE IF NOT EXISTS alert_notify_record{{if .Cluster}}_local ON CLUSTER {{.Cluster}}{{end}}
(

    `alert_id` String,

    `created_at` DateTime64(3),

    `event_id` String,

    `success` String,

    `failed` String
)
ENGINE {{if .Replication}}ReplicatedMergeTree{{else}}MergeTree(){{end}}
PARTITION BY toDate(created_at)
ORDER BY created_at
TTL toDateTime(created_at) + toIntervalDay({{.TTLDay}})
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1