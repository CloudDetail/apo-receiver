package metrics

import (
	"context"
	"fmt"
	"io"
	"log"
	"time"

	pb "github.com/CloudDetail/apo-receiver/internal/prometheus"
	"github.com/CloudDetail/apo-receiver/pkg/metrics/pm"
	"github.com/CloudDetail/apo-receiver/pkg/metrics/vm"
	"github.com/CloudDetail/apo-receiver/pkg/tenancy"
)

type Sender interface {
	SendMetrics(ctx context.Context, accountID string) error
}

func InitMetricSend(url string, interval int, promType string) error {
	var (
		sender Sender
		err    error
	)

	if promType == "vm" {
		collectMetrics := func(accountID string, w io.Writer) {
			GetMetrics(accountID, w)
		}
		sender, err = vm.NewVmPusher(url, collectMetrics)
	} else {
		buildMetricRequest := func() *pb.WriteRequest {
			return BuildPromWriteRequest()
		}
		sender, err = pm.NewPromRemoteWriter(url, buildMetricRequest)
	}

	if err != nil {
		return err
	}
	return InitMetricSendWithOptions(context.Background(), time.Duration(interval)*time.Second, sender)
}

func InitMetricSendWithOptions(ctx context.Context, interval time.Duration, sender Sender) error {
	// validate interval
	if interval <= 0 {
		return fmt.Errorf("interval must be positive; got %s", interval)
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		stopCh := ctx.Done()
		for {
			select {
			case <-ticker.C:
				ctxLocal, cancel := context.WithTimeout(ctx, interval+time.Second)
				tenants := GetUpdateTenant(ctxLocal)
				var err error
				for i := 0; i < len(tenants); i++ {
					err = sender.SendMetrics(ctxLocal, tenants[i].AccountID)
				}
				cancel()
				if err != nil {
					log.Printf("[x Send Metrics] %s", err)
				}
			case <-stopCh:
				return
			}
		}
	}()

	return nil
}

func GetUpdateTenant(ctx context.Context) []tenancy.TenantInfo {
	var tenants []tenancy.TenantInfo
	tenantScopeRegisteredMetrics.Range(func(key, value any) bool {
		tenant := key.(tenancy.TenantInfo)
		tenants = append(tenants, tenant)
		return true
	})
	return tenants
}
