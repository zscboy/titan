package api

import (
	"context"

	"github.com/Filecoin-Titan/titan/api/types"
)

// ContainerAPI is an interface for node
type ContainerAPI interface {
	GetRemoteAddress(ctx context.Context) (string, error)                                                        //perm:web,candidate,admin
	GetStatistics(ctx context.Context, id string) (*types.ResourcesStatistics, error)                            //perm:web,admin
	GetProviderList(ctx context.Context, option *types.GetProviderOption) ([]*types.Provider, error)             //perm:web,admin
	GetDeploymentList(ctx context.Context, opt *types.GetDeploymentOption) (*types.GetDeploymentListResp, error) //perm:web,candidate,admin
	GetDeploymentProviderIP(ctx context.Context, id types.DeploymentID) (string, error)                          //perm:edge,candidate,web,locator,admin
	CreateDeployment(ctx context.Context, deployment *types.Deployment) error                                    //perm:web,admin
	UpdateDeployment(ctx context.Context, deployment *types.Deployment) error                                    //perm:web,admin
	CloseDeployment(ctx context.Context, deployment *types.Deployment, force bool) error                         //perm:web,admin
	GetLogs(ctx context.Context, deployment *types.Deployment) ([]*types.ServiceLog, error)                      //perm:web,admin
	GetEvents(ctx context.Context, deployment *types.Deployment) ([]*types.ServiceEvent, error)                  //perm:web,admin
	SetProperties(ctx context.Context, properties *types.Properties) error                                       //perm:web,admin
	GetDeploymentDomains(ctx context.Context, id types.DeploymentID) ([]*types.DeploymentDomain, error)          //perm:web,admin
	AddDeploymentDomain(ctx context.Context, id types.DeploymentID, cert *types.Certificate) error               //perm:web,admin
	DeleteDeploymentDomain(ctx context.Context, id types.DeploymentID, domain string) error                      //perm:web,admin
	GetLeaseShellEndpoint(ctx context.Context, id types.DeploymentID) (*types.LeaseEndpoint, error)              //perm:web,admin
	GetIngress(ctx context.Context, id types.DeploymentID) (*types.Ingress, error)                               //perm:web,admin
	UpdateIngress(ctx context.Context, id types.DeploymentID, annotations map[string]string) error               //perm:web,admin
}
