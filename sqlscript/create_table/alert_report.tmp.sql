CREATE TABLE IF NOT EXISTS alert_report{{if .Cluster}}_local ON CLUSTER {{.Cluster}}{{end}}
(

    `report_id` UUID DEFAULT generateUUIDv4(),

    `report_type` LowCardinality(String) CODEC(ZSTD(1)),

    `overview_timestamp` DateTime64(3),

    `overview_detail` String CODEC(ZSTD(1)),

    `overview_reason` String CODEC(ZSTD(1)),

    `overview_tags` Map(String,
 String) CODEC(ZSTD(1)),

    `tags` Map(String,
 String) CODEC(ZSTD(1)),

    `topology` String CODEC(ZSTD(1)),

    `rootcause` String CODEC(ZSTD(1)),

    `rootcause_other` Array(String) CODEC(LZ4),

    `suggest` Array(String) CODEC(LZ4),

    `data` Array(String) CODEC(ZSTD(1))
)
ENGINE = {{if .Replication}}ReplicatedMergeTree{{else}}MergeTree(){{end}}
PARTITION BY toDate(overview_timestamp)
ORDER BY (report_type,overview_timestamp)
    TTL toDateTime(overview_timestamp) + toIntervalDay({{.TTLDay}})
    SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1