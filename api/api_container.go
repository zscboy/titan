package api

import (
	"context"

	"github.com/Filecoin-Titan/titan/api/types"
)

// ContainerAPI is an interface for node
type ContainerAPI interface {
	GetRemoteAddress(ctx context.Context) (string, error)                                                        //perm:web,candidate
	GetStatistics(ctx context.Context, id string) (*types.ResourcesStatistics, error)                            //perm:web
	GetProviderList(ctx context.Context, option *types.GetProviderOption) ([]*types.Provider, error)             //perm:web
	GetDeploymentList(ctx context.Context, opt *types.GetDeploymentOption) (*types.GetDeploymentListResp, error) //perm:web,candidate
	GetDeploymentProviderIP(ctx context.Context, id types.DeploymentID) (string, error)                          //perm:edge,candidate,web,locator
	CreateDeployment(ctx context.Context, deployment *types.Deployment) error                                    //perm:admin
	UpdateDeployment(ctx context.Context, deployment *types.Deployment) error                                    //perm:web
	CloseDeployment(ctx context.Context, deployment *types.Deployment, force bool) error                         //perm:web
	GetLogs(ctx context.Context, deployment *types.Deployment) ([]*types.ServiceLog, error)                      //perm:web
	GetEvents(ctx context.Context, deployment *types.Deployment) ([]*types.ServiceEvent, error)                  //perm:web
	SetProperties(ctx context.Context, properties *types.Properties) error                                       //perm:web
	GetDeploymentDomains(ctx context.Context, id types.DeploymentID) ([]*types.DeploymentDomain, error)          //perm:web
	AddDeploymentDomain(ctx context.Context, id types.DeploymentID, cert *types.Certificate) error               //perm:web
	DeleteDeploymentDomain(ctx context.Context, id types.DeploymentID, domain string) error                      //perm:web
	GetLeaseShellEndpoint(ctx context.Context, id types.DeploymentID) (*types.LeaseEndpoint, error)              //perm:web
	GetIngress(ctx context.Context, id types.DeploymentID) (*types.Ingress, error)                               //perm:web
	UpdateIngress(ctx context.Context, id types.DeploymentID, annotations map[string]string) error               //perm:web
}
