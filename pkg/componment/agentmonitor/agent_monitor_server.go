package agentmonitor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/CloudDetail/apo-receiver/pkg/model"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	pm "github.com/prometheus/common/model"
	"io/ioutil"
	"log"
	"net/http"
	"time"
)

type DingTalkMessage struct {
	Msgtype string `json:"msgtype"`
	Text    struct {
		Content string `json:"content"`
	} `json:"text"`
}

// 发送钉钉消息的函数
func sendDingTalkMessage(webhookURL, message string) error {
	// 创建消息结构体
	msg := DingTalkMessage{
		Msgtype: "text",
	}
	msg.Text.Content = message

	// 将消息结构体转换为 JSON 字节切片
	jsonData, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	// 创建 HTTP POST 请求
	req, err := http.NewRequest("POST", webhookURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")

	// 发送请求
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 读取响应内容
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// 检查响应状态码
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("请求失败，状态码: %d，响应内容: %s", resp.StatusCode, string(body))
	}

	return nil
}

type MonitorMetric struct {
	uId           string
	KernelVersion string
	Arch          string
	preEvtNum     uint64
	preCpuEvtNum  uint64
	preTxEvtNum   uint64
	MemUsage      uint64
	CpuUsage      uint64
	nowEvtNum     uint64
	nowCpuEvtNum  uint64
	nowTxEvtNum   uint64
}

type AgentMonitorServer struct {
	model.UnimplementedAgentMonitorServiceServer
	clusterId  string
	monitorIds map[string]MonitorMetric
	promClient v1.API
	dingDingWH string
}

func NewAgentMonitorServer(clusterId string, promClient v1.API, DingDingWH string) *AgentMonitorServer {
	ag := &AgentMonitorServer{
		clusterId:  clusterId,
		monitorIds: make(map[string]MonitorMetric),
		promClient: promClient,
		dingDingWH: DingDingWH,
	}
	go ag.StartStatistic()
	return ag
}

func queryMetricWithTime(ctx context.Context, api v1.API, query string) (float64, time.Time, error) {
	result, warnings, err := api.Query(ctx, query, time.Now())
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("query failed: %w", err)
	}
	if len(warnings) > 0 {
		log.Printf("Warnings: %v", warnings)
	}

	vector, ok := result.(pm.Vector)
	if !ok || len(vector) == 0 {
		return 0, time.Time{}, fmt.Errorf("no data found for query: %s", query)
	}

	latestSample := vector[0]

	// 将 Prometheus 时间戳（毫秒）转换为 time.Time
	timestamp := time.Unix(int64(latestSample.Timestamp)/1000, 0)
	return float64(latestSample.Value), timestamp, nil
}

func (server *AgentMonitorServer) SendAgentMonitorMetric(ctx context.Context, request *model.AgentMonitorRequest) (*model.AgentMonitorResponse, error) {

	rssQuery := fmt.Sprintf(
		`container_memory_rss{container="ebpf-agent", node_name="%s"}`,
		request.Iud,
	)
	fmt.Printf(rssQuery)
	rssValue, rssTime, err := queryMetricWithTime(ctx, server.promClient,
		rssQuery,
	)
	if err != nil {
		log.Fatalf("Error querying RSS: %v", err)
	}
	fmt.Printf("Latest RSS: %.2f MB (Time: %s)\n", rssValue/1024/1024, rssTime.Format(time.RFC3339))
	cpuQuery := fmt.Sprintf(
		`rate(container_cpu_usage_seconds_total{container="ebpf-agent", node_name="%s"}[1m]) * 100`,
		request.Iud,
	)
	// 查询 CPU 使用率及其时间戳
	cpuValue, cpuTime, err := queryMetricWithTime(ctx, server.promClient,
		cpuQuery,
	)
	fmt.Printf(cpuQuery)
	if err != nil {
		log.Fatalf("Error querying CPU: %v", err)
	}
	fmt.Printf("Latest CPU Usage: %.2f%% (Time: %s)\n", cpuValue, cpuTime.Format(time.RFC3339))
	request.CpuUsage = uint64(cpuValue)
	request.MemUsage = uint64(rssValue)
	// 组合消息内容
	if _, exists := server.monitorIds[request.Iud]; !exists {
		adptor := "已适配内核"
		if request.EvtNum == 0 {
			adptor = "未适配内核"
		}
		message := fmt.Sprintf("Agent 新发现（启动） %s：\n"+
			" - clusterId: %s\n"+
			" - Iud: %s\n"+
			" - KernelVersion: %s\n"+
			" - Arch: %s"+
			" - EvtNum: %d\n"+
			" - CpuEvtNum: %d\n"+
			" - TxEvtNum: %d\n"+
			" - MemUsage: %d mb\n"+
			" - CpuUsage: %d %%",
			adptor,
			server.clusterId,
			request.Iud,
			request.KernelVersion,
			request.Arch,
			request.EvtNum,
			request.CpuEvtNum,
			request.TxEvtNum,
			request.MemUsage/1000/1000,
			request.CpuUsage)
		metric := MonitorMetric{
			uId:           request.Iud,
			KernelVersion: request.KernelVersion,
			Arch:          request.Arch,
			preEvtNum:     request.EvtNum,
			preCpuEvtNum:  request.CpuEvtNum,
			preTxEvtNum:   request.TxEvtNum,
			MemUsage:      request.MemUsage / 1000 / 1000,
			CpuUsage:      request.CpuUsage,
			nowEvtNum:     request.EvtNum,
			nowCpuEvtNum:  request.CpuEvtNum,
			nowTxEvtNum:   request.TxEvtNum,
		}
		server.monitorIds[request.Iud] = metric

		// 替换为你自己的钉钉机器人 Webhook 地址
		webhookURL := server.dingDingWH
		log.Println("message:", message)
		// 发送消息
		err := sendDingTalkMessage(webhookURL, message)
		if err != nil {
			log.Println("发送钉钉消息失败:", err)
		}
	} else {
		metric := MonitorMetric{
			uId:           request.Iud,
			KernelVersion: request.KernelVersion,
			Arch:          request.Arch,
			preEvtNum:     server.monitorIds[request.Iud].nowEvtNum,
			preCpuEvtNum:  server.monitorIds[request.Iud].nowCpuEvtNum,
			preTxEvtNum:   server.monitorIds[request.Iud].nowTxEvtNum,
			MemUsage:      request.MemUsage / 1000 / 1000,
			CpuUsage:      request.CpuUsage,
			nowEvtNum:     request.EvtNum,
			nowCpuEvtNum:  request.CpuEvtNum,
			nowTxEvtNum:   request.TxEvtNum,
		}
		server.monitorIds[request.Iud] = metric
	}

	fileResp := &model.AgentMonitorResponse{}
	return fileResp, nil
}

func (server *AgentMonitorServer) doStatistic() {
	var validCountTmp int
	var totalEvtDiff, totalCpuEvtDiff, totalTxEvtDiff, totalCpu, totalMem uint64
	webhookURL := server.dingDingWH

	for uId, metric := range server.monitorIds {
		if metric.nowEvtNum > metric.preEvtNum {
			validCountTmp++
			totalEvtDiff += metric.nowEvtNum - metric.preEvtNum
			totalCpuEvtDiff += metric.nowCpuEvtNum - metric.preCpuEvtNum
			totalTxEvtDiff += metric.nowTxEvtNum - metric.preTxEvtNum
			totalCpu += metric.CpuUsage
			totalMem += metric.MemUsage
			metric.preEvtNum = metric.nowEvtNum
			server.monitorIds[uId] = metric
		} else {
			messageDeadAgent := fmt.Sprintf("Agent 事件不再增长，可能故障：\n"+
				" - clusterId: %s\n"+
				" - 探针id: %s\n",
				server.clusterId,
				metric.uId,
			)
			sendDingTalkMessage(webhookURL, messageDeadAgent)
		}
		if metric.CpuUsage > 100 || metric.MemUsage > 1000 {
			messageUnhealthAgent := fmt.Sprintf("Agent 资源过高：\n"+
				" - clusterId: %s\n"+
				" - 探针id: %s\n"+
				" - cpuuage: %d %%\n"+
				" - memage: %d mb \n ",
				server.clusterId,
				metric.uId,
				metric.CpuUsage,
				metric.MemUsage,
			)
			sendDingTalkMessage(webhookURL, messageUnhealthAgent)

		}
	}

	fmt.Printf("nowEvtNum - preEvtNum 大于 0 的 uid 个数: %d\n", validCountTmp)
	fmt.Printf("这些 uid 的 EvtNum 总差值: %d\n", totalEvtDiff)
	fmt.Printf("这些 uid 的 CpuEvtNum 总差值: %d\n", totalCpuEvtDiff)
	fmt.Printf("这些 uid 的 TxEvtNum 总差值: %d\n", totalTxEvtDiff)
	message := fmt.Sprintf("Agent 统计信息：\n"+
		" - clusterId: %s\n"+
		" - 正常运行探针个数: %d\n"+
		" - 每分钟平均事件数量: %d\n"+
		" - 每分钟平均Cpu事件数量: %d\n"+
		" - 每分钟平均tx事件数量: %d\n"+
		" - 平均cpu使用率: %d %%\n"+
		" - 平均内存: %d mb\n",
		server.clusterId,
		validCountTmp,
		totalEvtDiff/5,
		totalCpuEvtDiff/5,
		totalTxEvtDiff/5,
		totalCpu/uint64(validCountTmp),
		totalMem/uint64(validCountTmp),
	)
	log.Println("message:", message)
	// 发送消息
	sendDingTalkMessage(webhookURL, message)
}

func (server *AgentMonitorServer) StartStatistic() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			server.doStatistic()
		}
	}
}
