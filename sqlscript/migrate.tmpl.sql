-- 1.11.1
ALTER TABLE originx_app_info{{if .Cluster}}_local ON CLUSTER {{.Cluster}}{{end}} REMOVE TTL;
ALTER TABLE originx_app_info{{if .Cluster}}_local ON CLUSTER {{.Cluster}}{{end}} ADD COLUMN IF NOT EXISTS `heart_time` UInt64;
ALTER TABLE originx_app_info{{if .Cluster}}_local ON CLUSTER {{.Cluster}}{{end}} ADD COLUMN IF NOT EXISTS `heart_flag` UInt32;

-- 1.11.0
ALTER TABLE alert_event{{if .Cluster}}_local ON CLUSTER {{.Cluster}}{{end}} ADD COLUMN IF NOT EXISTS `event_id` String DEFAULT toString(id);

-- 1.9.0
ALTER TABLE workflow_records{{if .Cluster}}_local ON CLUSTER {{.Cluster}}{{end}} ADD COLUMN IF NOT EXISTS `alert_direction` String;
ALTER TABLE workflow_records{{if .Cluster}}_local ON CLUSTER {{.Cluster}}{{end}} ADD COLUMN IF NOT EXISTS `analyze_run_id` String;
ALTER TABLE workflow_records{{if .Cluster}}_local ON CLUSTER {{.Cluster}}{{end}} ADD COLUMN IF NOT EXISTS `analyze_err` String;

-- 1.5.0
ALTER TABLE workflow_records{{if .Cluster}}_local ON CLUSTER {{.Cluster}}{{end}} ADD COLUMN IF NOT EXISTS `rounded_time` DateTime64(3);

-- TODO Avoid re-create distributed table every-time collect restart
{{if .Cluster}}
drop table workflow_records on CLUSTER {{.Cluster}};
CREATE TABLE IF NOT EXISTS workflow_records
    ON CLUSTER {{.Cluster}} AS {{.Database}}.workflow_records_local
ENGINE = Distributed('{{.Cluster}}', '{{.Database}}', 'workflow_records_local', cityHash64(ref));
{{end}}

-- 1.3.0
ALTER TABLE alert_event{{if .Cluster}}_local ON CLUSTER {{.Cluster}}{{end}} ADD COLUMN IF NOT EXISTS `alert_id` String CODEC(ZSTD(1));
ALTER TABLE alert_event{{if .Cluster}}_local ON CLUSTER {{.Cluster}}{{end}} ADD COLUMN IF NOT EXISTS `raw_tags` Map(LowCardinality(String), String) CODEC(ZSTD(1));
ALTER TABLE alert_event{{if .Cluster}}_local ON CLUSTER {{.Cluster}}{{end}} ADD COLUMN IF NOT EXISTS `source_id` LowCardinality(String) CODEC(ZSTD(1));
ALTER TABLE alert_event{{if .Cluster}}_local ON CLUSTER {{.Cluster}}{{end}} MODIFY COLUMN IF EXISTS `tags` Map(LowCardinality(String), String) CODEC(ZSTD(1));

{{if .Cluster}}
drop table alert_event on CLUSTER {{.Cluster}};
CREATE TABLE IF NOT EXISTS alert_event
    ON CLUSTER {{.Cluster}} AS {{.Database}}.alert_event_local
ENGINE = Distributed('{{.Cluster}}', '{{.Database}}', 'alert_event_local', cityHash64(alert_id));
{{end}}