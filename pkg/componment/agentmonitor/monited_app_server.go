package agentmonitor

import (
	"context"
	"log"

	"github.com/CloudDetail/apo-receiver/pkg/global"
	grpc_model "github.com/CloudDetail/apo-receiver/pkg/model"
)

type MonitedAppServer struct {
	grpc_model.UnimplementedAppServiceServer
}

func NewMonitedAppServer() *MonitedAppServer {
	return &MonitedAppServer{}
}

// TODO Remove this function.
func (server *MonitedAppServer) QueryMonitedApps(ctx context.Context, request *grpc_model.QueryMonitedAppRequest) (*grpc_model.QueryMonitedAppResponse, error) {
	return &grpc_model.QueryMonitedAppResponse{
		Datas: []*grpc_model.QueryMonitedAppData{},
	}, nil
}

func (server *MonitedAppServer) QueryActiveApps(ctx context.Context, request *grpc_model.QueryActiveAppRequest) (*grpc_model.QueryActiveAppResponse, error) {
	if len(request.ActiveApps) > 0 {
		if err := global.CLICK_HOUSE.UpdateAppHeartTimes(request.NodeIp, request.NodeName, request.ActiveApps); err != nil {
			log.Printf("[x Update App HeartTime] NodeIp: %s, NodeName: %s, Error: %s", request.NodeIp, request.NodeName, err.Error())
		}
	}
	if len(request.DeadApps) > 0 {
		if err := global.CLICK_HOUSE.UpdateAppDeadFlags(request.NodeIp, request.NodeName, request.DeadApps); err != nil {
			log.Printf("[x Update App IsDead] NodeIp: %s, NodeName: %s, Error: %s", request.NodeIp, request.NodeName, err.Error())
		}
	}

	result, err := global.CLICK_HOUSE.QueryActiveApps(ctx, request.NodeIp, request.NodeName, request.DeadApps)
	if err != nil {
		log.Printf("[x Query Active Apps] NodeIp: %s, NodeName: %s, Error: %s", request.NodeIp, request.NodeName, err.Error())
	}
	return &grpc_model.QueryActiveAppResponse{
		Datas: result,
	}, err
}
