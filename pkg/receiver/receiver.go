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
	receiverCfg, sampleCfg, profileCfg, prometheusCfg, clickHouseCfg, analyzerCfg, redisCfg, k8sCfg, err := readInConfig(*configPath)
	if err != nil {
		return fmt.Errorf("fail to read configuration: %w", err)
	}

	if redisCfg.Enable {
		redisClient, err := redis.NewRedisClient(redisCfg.Address, redisCfg.Password, redisCfg.ExpireTime)
		if err != nil {
			return fmt.Errorf("fail to create redis client: %w", err)
		}
		global.CACHE = redisClient
	} else {
		global.CACHE = redis.NewLocalCache(redisCfg.ExpireTime)
	}
	global.CACHE.Start()

	global.TRACE_CLIENT = client.NewApmTraceClient(
		analyzerCfg.TraceAddress,
		analyzerCfg.Timeout,
		analyzerCfg.RatioThreshold,
		analyzerCfg.MuateNodeMode,
		analyzerCfg.GetDetailTypes)

	clickHouseClient, err := clickhouse.NewClickHouseClient(ctx, clickHouseCfg, prometheusCfg.GenerateClientMetric, prometheusCfg.ClientMetricWithUrl)
	if err != nil {
		return fmt.Errorf("fail to create ClickHouse client: %w", err)
	}
	global.CLICK_HOUSE = clickHouseClient
	clickHouseClient.Start()

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
	prometheusV1Api := v1.NewAPI(prometheusClient)

	portalClient := httphelper.CreateHttpClient(receiverCfg.PortalAddress != "", receiverCfg.PortalAddress)
	slomanager.InitDefaultSLOConfigCache(receiverCfg.CenterApiServer, portalClient, prometheusCfg.Address)

	threshold.CacheInstance = threshold.NewThresholdCache(prometheusV1Api, sloconfig.DefaultConfigCache)
	threshold.CacheInstance.Start()

	onoffmetric.CacheInstance = onoffmetric.NewMetricCache(prometheusV1Api)
	onoffmetric.CacheInstance.Start()

	startMetadataFetch(k8sCfg)

	// Start gRPC server
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		startGrpcServer(receiverCfg, sampleCfg, profileCfg, analyzerCfg, threshold.CacheInstance)
	}()
	// Start HTTP server
	wg.Add(1)
	go func() {
		defer wg.Done()
		httpserver.StartHttpServer(receiverCfg.HttpPort, prometheusCfg.OpenApiMetrics)
	}()
	if prometheusCfg.SendApi != "" && prometheusCfg.SendInterval > 0 {
		promSendAddress := prometheusCfg.SendAddress
		if promSendAddress == "" {
			// fix for earlier version.
			promSendAddress = prometheusCfg.Address
		}
		if err := metrics.InitMetricSend(fmt.Sprintf("%s%s", promSendAddress, prometheusCfg.SendApi), prometheusCfg.SendInterval, prometheusCfg.Storage); err != nil {
			return err
		}
	}

	// Wait for the two servers shutting down
	wg.Wait()
	log.Println("All servers shut down gracefully")
	return nil
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
	thresholdCache *threshold.ThresholdCache) {
	listen, err := net.Listen("tcp", ":"+strconv.Itoa(receiverCfg.GrpcPort))
	if err != nil {
		log.Fatalf("Fail to listen Grpc Port: %v\n", err)
	}

	server := grpc.NewServer()

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
