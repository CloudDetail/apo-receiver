package agentmonitor

import (
	"context"

	grpc_model "github.com/CloudDetail/apo-receiver/pkg/model"
)

type MonitedAppServer struct {
	grpc_model.UnimplementedAppServiceServer
}

func NewMonitedAppServer() *MonitedAppServer {
	return &MonitedAppServer{
	}
}

func (server *MonitedAppServer) QueryMonitedApps(ctx context.Context, request *grpc_model.QueryMonitedAppRequest) (*grpc_model.QueryMonitedAppResponse, error) {
	return &grpc_model.QueryMonitedAppResponse{
		Datas: CacheInstance.GetMonitedApps(request.NodeIp, request.NodeName, request.ClusterId),
	}, nil
}
