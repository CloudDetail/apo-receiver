CREATE TABLE IF NOT EXISTS alert_event{{if .Cluster}}_local ON CLUSTER {{.Cluster}}{{end}}
(
    `source` String CODEC(ZSTD(1)),

    `group` String,

    `event_id` String,

    `id` UUID,

    `create_time` DateTime64(3),

    `update_time` DateTime64(3),

    `end_time` DateTime64(3),

    `received_time` DateTime64(3),

    `severity` Enum8('unknown' = 0, 'info' = 1, 'warning' = 2, 'error' = 3, 'critical' = 4),

    `name` String,

    `detail` String,

    `tags` Map(LowCardinality(String), String) CODEC(ZSTD(1)),

    `status` Enum8('resolved' = 0, 'firing' = 1),

    `alert_id` String CODEC(ZSTD(1)),

    `raw_tags` Map(String,String) CODEC(ZSTD(1)),

    `source_id` LowCardinality(String) CODEC(ZSTD(1))
) ENGINE {{if .Replication}}ReplicatedMergeTree{{else}}MergeTree(){{end}}
PARTITION BY toDate(received_time)
 ORDER BY (source, received_time)
    TTL toDateTime(received_time) + toIntervalDay({{.TTLDay}})
    SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1