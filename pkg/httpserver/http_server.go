package httpserver

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/kataras/iris/v12"
	"github.com/kataras/iris/v12/middleware/pprof"

	"github.com/CloudDetail/apo-receiver/pkg/componment/threshold"
	"github.com/CloudDetail/apo-receiver/pkg/global"
	"github.com/CloudDetail/apo-receiver/pkg/metrics"
	"github.com/CloudDetail/apo-receiver/pkg/tenancy"

	slomodel "github.com/CloudDetail/apo-module/slo/api/v1/model"
	sloconfig "github.com/CloudDetail/apo-module/slo/sdk/v1/config"
)

func StartHttpServer(port int, openMetricsApi bool, authExtension *tenancy.AuthExtension) {
	app := iris.Default()

	if authExtension != nil {
		app.Use(authExtension.AuthMiddleware())
	}

	if openMetricsApi {
		app.Get("/metrics", getPromMetrics)
	}
	app.Post("/config/slo", setSLOConfig)
	app.Get("/debug/thresholds", getThresholds)
	app.Get("/realtimereport/slow/{traceId:string}", realtimeSlowReport)
	app.Get("/realtimereport/error/{traceId:string}", realtimeErrorReport)

	p := pprof.New()
	app.Any("/debug/pprof", p)
	app.Any("/debug/pprof/{action:path}", p)

	// Graceful shutdown
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		<-c
		log.Println("Shutting down HTTP server...")
		_ = app.Shutdown(context.Background())
	}()

	err := app.Listen(":" + strconv.Itoa(port))
	if err != nil {
		log.Fatalf("Failed to start the http server %v", err)
	}
}

type BasicStatus string

const (
	Success BasicStatus = "success"
	Failure BasicStatus = "failure"
)

type BasicResponse struct {
	Status  BasicStatus `json:"status"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

type SLOConfigRequest struct {
	EntryUri   string               `json:"entryUri"`
	SLOConfigs []slomodel.SLOConfig `json:"sloConfigs"`
}

func setSLOConfig(ctx iris.Context) {
	var request SLOConfigRequest

	err := ctx.ReadJSON(&request)
	log.Printf("setSLOConfig: %v", request)
	if err != nil {
		ctx.StopWithStatus(iris.StatusInternalServerError)
		_ = ctx.JSON(BasicResponse{
			Status:  Failure,
			Message: err.Error(),
		})
		return
	}

	// Update the SLO cache
	sloconfig.AddOrUpdateSLOTarget(slomodel.SLOEntryKey{EntryURI: request.EntryUri}, request.SLOConfigs)
	// Update the slow threshold cache
	slowThreshold := threshold.GetSlowThresholdFromSLOs(request.EntryUri, request.SLOConfigs)
	threshold.CacheInstance.UpdateThresholdConfig(slowThreshold.Url, slowThreshold)
	_ = ctx.JSON(BasicResponse{
		Status: Success,
		Data:   nil,
	})
}

func getThresholds(ctx iris.Context) {
	_ = ctx.JSON(BasicResponse{
		Status: Success,
		Data:   threshold.CacheInstance.SlowThresholdMap,
	})
}

func realtimeSlowReport(ctx iris.Context) {
	traceId := ctx.Params().GetString("traceId")

	traces, err := global.CLICK_HOUSE.QueryTraces(ctx, traceId)
	if err != nil {
		responseWithError(ctx, err)
		return
	}

	result, clientCalls, err := global.TRACE_CLIENT.QueryMutatedSlowTraceTree(traceId, traces)
	if err != nil {
		responseWithError(ctx, err)
		return
	}
	ctx.JSON(iris.Map{
		"success": true,
		"data":    result,
		"client":  clientCalls,
	})
}

func realtimeErrorReport(ctx iris.Context) {
	traceId := ctx.Params().GetString("traceId")

	traces, err := global.CLICK_HOUSE.QueryTraces(ctx, traceId)
	if err != nil {
		responseWithError(ctx, err)
		return
	}

	result, err := global.TRACE_CLIENT.QueryErrorTraceTree(traceId, traces)
	if err != nil {
		responseWithError(ctx, err)
		return
	}
	ctx.JSON(iris.Map{
		"success": true,
		"data":    result,
	})
}

func getPromMetrics(ctx iris.Context) {
	tenant := tenancy.GetTenant(ctx)
	metrics.GetMetrics(tenant.AccountID, ctx.ResponseWriter())
}

func responseWithError(ctx iris.Context, err error) {
	ctx.StopWithStatus(iris.StatusInternalServerError)
	ctx.JSON(iris.Map{
		"success":  false,
		"errorMsg": err.Error(),
	})
}
