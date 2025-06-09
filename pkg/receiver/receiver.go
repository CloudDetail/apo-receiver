package receiver

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"

	"github.com/CloudDetail/apo-receiver/pkg/componment/agentmonitor"
	"github.com/CloudDetail/apo-receiver/pkg/tenancy"

	"github.com/CloudDetail/apo-receiver/pkg/componment/ebpffile"
	"github.com/CloudDetail/apo-receiver/pkg/componment/redis"
	"github.com/CloudDetail/apo-receiver/pkg/httphelper"
	"github.com/CloudDetail/apo-receiver/pkg/httpserver"
	"github.com/CloudDetail/apo-receiver/pkg/metrics"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/spf13/viper"
	"google.golang.org/grpc"

	"github.com/CloudDetail/apo-receiver/pkg/analyzer"
	"github.com/CloudDetail/apo-receiver/pkg/clickhouse"
	"github.com/CloudDetail/apo-receiver/pkg/componment/onoffmetric"
	"github.com/CloudDetail/apo-receiver/pkg/componment/profile"
	"github.com/CloudDetail/apo-receiver/pkg/componment/threshold"
	"github.com/CloudDetail/apo-receiver/pkg/componment/trace"
	"github.com/CloudDetail/apo-receiver/pkg/config"
	"github.com/CloudDetail/apo-receiver/pkg/global"
	"github.com/CloudDetail/apo-receiver/pkg/model"

	"github.com/CloudDetail/apo-module/apm/client/v1"
	sloconfig "github.com/CloudDetail/apo-module/slo/sdk/v1/config"
	slomanager "github.com/CloudDetail/apo-module/slo/sdk/v1/manager"
	"github.com/CloudDetail/metadata/source"
)

func Run(ctx context.Context) error {
	// Initialize flags
	configPath := flag.String("config", "receiver-config.yml", "Configuration file")
	flag.Parse()

	cfg, err := readInConfigNew(*configPath)
	// receiverCfg, sampleCfg, profileCfg, prometheusCfg, clickHouseCfg, analyzerCfg, redisCfg, k8sCfg, err := readInConfig(*configPath)
	if err != nil {
		return fmt.Errorf("fail to read configuration: %w", err)
	}

	tenancyCfg := &cfg.TenancyCfg
	authExtension, err := tenancy.NewAuthExtension(tenancyCfg)
	if err != nil {
		return fmt.Errorf("fail to create authExtension: %w", err)
	}

	redisCfg := &cfg.RedisCfg
	if cfg.RedisCfg.Enable {
		redisClient, err := redis.NewRedisClient(redisCfg.Address, redisCfg.Password, redisCfg.ExpireTime)
		if err != nil {
			return fmt.Errorf("fail to create redis client: %w", err)
		}
		global.CACHE = redisClient
	} else {
		global.CACHE = redis.NewLocalCache(redisCfg.ExpireTime)
	}
	global.CACHE.Start()

	analyzerCfg := &cfg.AnalyzerCfg
	traceAPI := client.NewAdapterHTTPClient(
		analyzerCfg.TraceAddress,
		analyzerCfg.Timeout,
	)
	if cfg.TenancyCfg.Enabled {
		traceAPI.SetRountTriper(authExtension)
	}

	global.TRACE_CLIENT = client.NewApmTraceClientByAPI(
		traceAPI,
		analyzerCfg.RatioThreshold,
		analyzerCfg.MuateNodeMode,
		analyzerCfg.GetDetailTypes)

	prometheusCfg := &cfg.PrometheusCfg
	if len(prometheusCfg.LatencyHistogramBuckets) == 0 && prometheusCfg.Storage == "prom" && prometheusCfg.GenerateClientMetric {
		return errors.New("miss latency_histogram_buckets for promethues")
	}
	metrics.UpdateMetricConfig(prometheusCfg.Storage, prometheusCfg.CacheSize, prometheusCfg.LatencyHistogramBuckets)
	global.PROM_RANGE = prometheusCfg.GetRange()
	prometheusClient, err := api.NewClient(api.Config{
		Address: prometheusCfg.Address,
	})
	if err != nil {
		return fmt.Errorf("fail to create Prometheus client: %w", err)
	}
	log.Printf("Use the prometheus address %v", prometheusCfg.Address)

	if cfg.TenancyCfg.Enabled {
		if prometheusCfg.Storage != "vm" {
			return errors.New("prometheus storage must be vm when tenancy is enabled")
		}
		prometheusClient = &tenancy.TenantClient{
			Client: prometheusClient,
		}
	}

	prometheusV1Api := v1.NewAPI(prometheusClient)

	clickHouseCfg := &cfg.ClickHouseCfg
	clickHouseClient, err := clickhouse.NewClickHouseClient(ctx, clickHouseCfg, prometheusCfg.GenerateClientMetric, prometheusCfg.ClientMetricWithUrl, cfg.TenancyCfg.Enabled)
	if err != nil {
		return fmt.Errorf("fail to create ClickHouse client: %w", err)
	}
	global.CLICK_HOUSE = clickHouseClient
	clickHouseClient.Start()

	receiverCfg := &cfg.ReceiverCfg
	portalClient := httphelper.CreateHttpClient(receiverCfg.PortalAddress != "", receiverCfg.PortalAddress)
	slomanager.InitDefaultSLOConfigCache(receiverCfg.CenterApiServer, portalClient, prometheusCfg.Address)

	threshold.CacheInstance = threshold.NewThresholdCache(prometheusV1Api, sloconfig.DefaultConfigCache)
	threshold.CacheInstance.Start()

	onoffmetric.CacheInstance = onoffmetric.NewMetricCache(prometheusV1Api)
	onoffmetric.CacheInstance.Start()

	k8sCfg := &cfg.K8sCfg
	startMetadataFetch(k8sCfg)

	sampleCfg := &cfg.SampleConfig
	profileCfg := &cfg.ProfileCfg
	// Start gRPC server
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		startGrpcServer(receiverCfg, sampleCfg, profileCfg, analyzerCfg, threshold.CacheInstance, prometheusV1Api, authExtension)
	}()
	// Start HTTP server
	wg.Add(1)
	go func() {
		defer wg.Done()
		httpserver.StartHttpServer(receiverCfg.HttpPort, prometheusCfg.OpenApiMetrics, authExtension)
	}()
	if prometheusCfg.SendApi != "" && prometheusCfg.SendInterval > 0 {
		promSendAddress := prometheusCfg.SendAddress
		if promSendAddress == "" {
			// fix for earlier version.
			promSendAddress = prometheusCfg.Address
		}
		promRemoteWriteType := prometheusCfg.RemoteWriteType
		if promRemoteWriteType == "" {
			// fix for earlier version.
			promRemoteWriteType = prometheusCfg.Storage
		}

		if tenancyCfg.Enabled {
			if prometheusCfg.Storage != "vm" {
				return errors.New("prometheus storage must be vm when tenancy is enabled")
			}
			prometheusCfg.SendApi = fmt.Sprintf("%s%s", `/insert/:accountID/prometheus`, prometheusCfg.SendApi)
		}

		if err := metrics.InitMetricSend(fmt.Sprintf("%s%s", promSendAddress, prometheusCfg.SendApi), prometheusCfg.SendInterval, promRemoteWriteType); err != nil {
			return err
		}
	}

	// Wait for the two servers shutting down
	wg.Wait()
	log.Println("All servers shut down gracefully")
	return nil
}

func readInConfigNew(path string) (*config.Config, error) {
	viper := viper.New()
	viper.SetConfigFile(path)
	err := viper.ReadInConfig()
	if err != nil {
		return nil, err
	}

	config := &config.Config{}
	_ = viper.Unmarshal(config)
	return config, nil
}

func readInConfig(path string) (*config.ReceiverConfig, *config.SampleConfig, *config.ProfileConfig, *config.PrometheusConfig, *config.ClickHouseConfig, *config.AnalyzerConfig, *config.RedisConfig, *config.K8sConfig, error) {
	viper := viper.New()
	viper.SetConfigFile(path)
	err := viper.ReadInConfig()
	if err != nil { // Handle errors reading the config file
		return nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("error happened while reading config file: %w", err)
	}
	receiverCfg := &config.ReceiverConfig{}
	_ = viper.UnmarshalKey("receiver", receiverCfg)

	sampleCfg := &config.SampleConfig{}
	_ = viper.UnmarshalKey("sample", sampleCfg)

	profileCfg := &config.ProfileConfig{}
	_ = viper.UnmarshalKey("profile", profileCfg)

	prometheusCfg := &config.PrometheusConfig{}
	_ = viper.UnmarshalKey("promethues", prometheusCfg)

	clickHouseCfg := &config.ClickHouseConfig{}
	_ = viper.UnmarshalKey("clickhouse", clickHouseCfg)

	analyzerCfg := &config.AnalyzerConfig{}
	_ = viper.UnmarshalKey("analyzer", analyzerCfg)

	redisCfg := &config.RedisConfig{}
	_ = viper.UnmarshalKey("redis", redisCfg)

	k8sCfg := &config.K8sConfig{}
	_ = viper.UnmarshalKey("k8s", k8sCfg)

	return receiverCfg, sampleCfg, profileCfg, prometheusCfg, clickHouseCfg, analyzerCfg, redisCfg, k8sCfg, nil
}

func startGrpcServer(
	receiverCfg *config.ReceiverConfig,
	sampleCfg *config.SampleConfig,
	profileCfg *config.ProfileConfig,
	analyzerCfg *config.AnalyzerConfig,
	thresholdCache *threshold.ThresholdCache,
	promClient v1.API,
	authExtension *tenancy.AuthExtension,
) {
	listen, err := net.Listen("tcp", ":"+strconv.Itoa(receiverCfg.GrpcPort))
	if err != nil {
		log.Fatalf("Fail to listen Grpc Port: %v\n", err)
	}

	var grpcOpts []grpc.ServerOption

	if authExtension != nil {
		grpcOpts = append(grpcOpts,
			grpc.StreamInterceptor(authExtension.NewGuardingStreamInterceptor()),
			grpc.UnaryInterceptor(authExtension.NewGuardingUnaryInterceptor()),
		)
	}

	server := grpc.NewServer(grpcOpts...)

	sampleServer := trace.NewSampleServer(sampleCfg.Enable, sampleCfg.MinSample, sampleCfg.InitSample, sampleCfg.MaxSample, sampleCfg.ResetSamplePeriod)
	model.RegisterSampleServiceServer(server, sampleServer)
	sampleServer.Start()

	log.Printf("Start Profile Cache Time: %d", profileCfg.TraceIdCacheTime)
	profileServer := profile.NewProfileServer(profileCfg.TraceIdCacheTime, profileCfg.OpenWindowSample, profileCfg.WindowSampleNum)
	model.RegisterProfileServiceServer(server, profileServer)
	profileServer.Start()

	thresholdServer := threshold.NewThresholdServer(thresholdCache)
	model.RegisterSlowThresholdServiceServer(server, thresholdServer)

	analyzer := analyzer.NewReportAnalyzer(analyzerCfg, profileServer.SignalsCache)

	traceServer := trace.NewTraceServer(analyzer)
	model.RegisterTraceServiceServer(server, traceServer)
	traceServer.Start()

	ebpfFileReceiver := ebpffile.NewEbpfFIleServer(receiverCfg.CenterApiServer, receiverCfg.PortalAddress)
	model.RegisterFileServiceServer(server, ebpfFileReceiver)

	agentMonitorReceiver := agentmonitor.NewAgentMonitorServer(receiverCfg.ClusterId, promClient, receiverCfg.DingDingWH)
	model.RegisterAgentMonitorServiceServer(server, agentMonitorReceiver)

	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)

		<-c
		log.Println("Shutting down gRPC server...")

		server.GracefulStop()

		_ = listen.Close()
		log.Println("gRPC server closed.")
	}()

	log.Printf("Start Grpc Server: %d", receiverCfg.GrpcPort)
	if err := server.Serve(listen); err != nil {
		log.Fatalf("Fail to start server: %v", err)
	}
}

func startMetadataFetch(k8sCfg *config.K8sConfig) {
	if !k8sCfg.Enable {
		return
	}

	// TODO CheckAPIType
	source := source.CreateMetaSourceFromConfig(k8sCfg.MetaServerConfig)
	err := source.Run()
	if err != nil {
		log.Printf("Fail to start metadata fetch: %v", err)
	}
}
