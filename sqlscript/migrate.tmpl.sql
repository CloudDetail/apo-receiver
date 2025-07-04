-- 1.9.0
ALTER TABLE workflow_records{{if .Cluster}}_local ON CLUSTER {{.Cluster}}{{end}} ADD COLUMN `alert_direction` String;
ALTER TABLE workflow_records{{if .Cluster}}_local ON CLUSTER {{.Cluster}}{{end}} ADD COLUMN `analyze_run_id` String;
ALTER TABLE workflow_records{{if .Cluster}}_local ON CLUSTER {{.Cluster}}{{end}} ADD COLUMN `analyze_err` String;

-- 1.5.0
ALTER TABLE workflow_records{{if .Cluster}}_local ON CLUSTER {{.Cluster}}{{end}} ADD COLUMN `rounded_time` DateTime64(3);

{{if .Cluster}}
drop table workflow_records on CLUSTER {{.Cluster}};
CREATE TABLE IF NOT EXISTS workflow_records
    ON CLUSTER {{.Cluster}} AS {{.Database}}.workflow_records_local
ENGINE = Distributed('{{.Cluster}}', '{{.Database}}', 'workflow_records_local', cityHash64(ref));
{{end}}

-- 1.3.0
ALTER TABLE alert_event{{if .Cluster}}_local ON CLUSTER {{.Cluster}}{{end}} ADD COLUMN `alert_id` String CODEC(ZSTD(1));
ALTER TABLE alert_event{{if .Cluster}}_local ON CLUSTER {{.Cluster}}{{end}} ADD COLUMN `raw_tags` Map(LowCardinality(String), String) CODEC(ZSTD(1));
ALTER TABLE alert_event{{if .Cluster}}_local ON CLUSTER {{.Cluster}}{{end}} ADD COLUMN `source_id` LowCardinality(String) CODEC(ZSTD(1));
ALTER TABLE alert_event{{if .Cluster}}_local ON CLUSTER {{.Cluster}}{{end}} MODIFY COLUMN `tags` Map(LowCardinality(String), String) CODEC(ZSTD(1));

{{if .Cluster}}
drop table alert_event on CLUSTER {{.Cluster}};
CREATE TABLE IF NOT EXISTS alert_event
    ON CLUSTER {{.Cluster}} AS {{.Database}}.alert_event_local
ENGINE = Distributed('{{.Cluster}}', '{{.Database}}', 'alert_event_local', cityHash64(alert_id));
{{end}}