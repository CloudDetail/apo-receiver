CREATE TABLE IF NOT EXISTS service_red_metric{{if .Cluster}}_local ON CLUSTER {{.Cluster}}{{end}}
(
    timestamp DateTime CODEC(Delta, ZSTD(1)),
    cluster_id String CODEC(ZSTD(1)),
    source String CODEC(ZSTD(1)),
    service_id String CODEC(ZSTD(1)),
    service_name String CODEC(ZSTD(1)),
    endpoint String CODEC(ZSTD(1)),
    count UInt64,
    error_count UInt64,
    duration UInt64,
    INDEX idx_service service_name TYPE bloom_filter(0.01) GRANULARITY 1
) ENGINE {{if .Replication}}ReplicatedMergeTree{{else}}MergeTree(){{end}}
    PARTITION BY toDate(timestamp)
    ORDER BY toUnixTimestamp(timestamp)
    TTL toDateTime(timestamp) + toIntervalDay({{.TTLDay}})
    SETTINGS index_granularity=8192, ttl_only_drop_parts = 1