package config

import (
	"time"

	metaconfigs "github.com/CloudDetail/metadata/configs"
)

type Config struct {
	ReceiverCfg   *ReceiverConfig
	ProfileCfg    *ProfileConfig
	PrometheusCfg *PrometheusConfig
	ClickHouseCfg *ClickHouseConfig
	AnalyzerCfg   *AnalyzerConfig
	RedisCfg      *RedisConfig
	K8sCfg        *K8sConfig
}

type ReceiverConfig struct {
	GrpcPort        int    `mapstructure:"grpc_port"`
	HttpPort        int    `mapstructure:"http_port"`
	CenterApiServer string `mapstructure:"center_api_server"`
	PortalAddress   string `mapstructure:"portal_address"`
}

type SampleConfig struct {
	Enable            bool          `mapstructure:"enable"`
	MinSample         int64         `mapstructure:"min_sample"`
	InitSample        int64         `mapstructure:"init_sample"`
	MaxSample         int64         `mapstructure:"max_sample"`
	ResetSamplePeriod time.Duration `mapstructure:"reset_sample_period"`
}

type ProfileConfig struct {
	TraceIdCacheTime int  `mapstructure:"traceid_cache_time"`
	OpenWindowSample bool `mapstructure:"open_window_sample"`
	WindowSampleNum  int  `mapstructure:"window_sample_num"`
}

type PrometheusConfig struct {
	Address                 string          `mapstructure:"address"`
	Storage                 string          `mapstructure:"storage"`
	CacheSize               int             `mapstructure:"cache_size"`
	LatencyHistogramBuckets []time.Duration `mapstructure:"latency_histogram_buckets"`
	SendAddress             string          `mapstructure:"send_address"`
	SendApi                 string          `mapstructure:"send_api"`
	SendInterval            int             `mapstructure:"send_interval"`
	GenerateClientMetric    bool            `mapstructure:"generate_client_metric"`
	ClientMetricWithUrl     bool            `mapstructure:"client_metric_with_url"`
	OpenApiMetrics          bool            `mapstructure:"open_api_metrics"`
}

func (promqCfg *PrometheusConfig) GetRange() string {
	if promqCfg.Storage == "prom" {
		return "le"
	}
	return "vmrange"
}

type ClickHouseConfig struct {
	// Endpoint is the clickhouse endpoint.
	Endpoint string `mapstructure:"endpoint"`
	// Username is the authentication username.
	Username string `mapstructure:"username"`
	// Password is the authentication password.
	Password string `mapstructure:"password"`
	// Database is the database name to export.
	Database string `mapstructure:"database"`
	// Replication decides whether to create a replicated table.
	Replication bool `mapstructure:"replication"`
	// Cluster if set will append `ON CLUSTER` with the provided name when creating tables.
	Cluster string `mapstructure:"cluster"`
	// TTLDays is The data time-to-live in days, 0 means no ttl.
	TTLDays uint `mapstructure:"ttl_days"`
	// TTLConfigs
	TTLConfig  []*TTLConfig  `mapstructure:"ttl_config"`
	HashConfig []*HashConfig `mapstructure:"hash_config"`
	// If Not set will be set to 5.
	FlushSeconds        uint `mapstructure:"flush_seconds"`
	ExportServiceClient bool `mapstructure:"export_service_client"`
}

type TTLConfig struct {
	Tables []string `mapstructure:"tables"`
	TTL    uint     `mapstructure:"ttl"`
}

type HashConfig struct {
	Tables []string `mapstructure:"tables"`
	Hash   string   `mapstructure:"hash"`
}

type AnalyzerConfig struct {
	ThreadCount    int      `mapstructure:"thread_count"`
	DelayDuration  int64    `mapstructure:"delay_duration"`
	RetryDuration  int64    `mapstructure:"retry_duration"`
	RetryTimes     int      `mapstructure:"retry_times"`
	MissTopTime    int64    `mapstructure:"miss_top_time"`
	TopologyPeriod uint64   `mapstructure:"topology_period"`
	RatioThreshold int      `mapstructure:"ratio_threshold"`
	SegmentSize    int      `mapstructure:"segment_size"`
	MuateNodeMode  string   `mapstructure:"mutate_node_mode"`
	TraceAddress   string   `mapstructure:"trace_adress"`
	Timeout        int64    `mapstructure:"timeout"`
	GetDetailTypes []string `mapstructure:"get_detail_types"`
	HttpParser     string   `mapstructure:"http_parser"`
}

type RedisConfig struct {
	Enable     bool   `mapstructure:"enable"`
	Address    string `mapstructure:"address"`
	Password   string `mapstructure:"password"`
	ExpireTime int64  `mapstructure:"expire_time"`
}

type K8sConfig struct {
	Enable  bool   `mapstructure:"enable"`
	APIType string `mapstructure:"api_type"` // meta_server

	MetaServerConfig *metaconfigs.MetaSourceConfig `mapstructure:"meta_server_config"`
}
