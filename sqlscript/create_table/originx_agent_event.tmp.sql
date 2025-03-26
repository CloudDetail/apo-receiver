CREATE TABLE IF NOT EXISTS originx_agent_event{{if .Cluster}}_local ON CLUSTER {{.Cluster}}{{end}}
(
    timestamp DateTime64(9) CODEC(Delta, ZSTD(1)),
    name String CODEC(ZSTD(1)),
    pid UInt32,
    labels Map(LowCardinality(String), String) CODEC(ZSTD(1)),
    status Bool,
    INDEX idx_pid pid TYPE minmax GRANULARITY 1
) ENGINE {{if .Replication}}ReplicatedMergeTree{{else}}MergeTree(){{end}}
    PARTITION BY toDate(timestamp)
    ORDER BY toUnixTimestamp(timestamp)
    TTL toDateTime(timestamp) + toIntervalDay({{.TTLDay}})
    SETTINGS index_granularity=8192, ttl_only_drop_parts = 1