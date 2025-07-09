CREATE TABLE IF NOT EXISTS service_topology{{if .Cluster}}_local ON CLUSTER {{.Cluster}}{{end}}
(
    timestamp DateTime CODEC(Delta, ZSTD(1)),
    source String CODEC(ZSTD(1)),
    parent_service String CODEC(ZSTD(1)),
    parent_type String CODEC(ZSTD(1)),
    child_service String CODEC(ZSTD(1)),
    child_type String CODEC(ZSTD(1))
) ENGINE {{if .Replication}}ReplicatedMergeTree{{else}}MergeTree(){{end}}
    PARTITION BY toDate(timestamp)
    ORDER BY toUnixTimestamp(timestamp)
    TTL toDateTime(timestamp) + toIntervalDay({{.TTLDay}})
    SETTINGS index_granularity=8192, ttl_only_drop_parts = 1