package types

import (
	"time"

	"github.com/Filecoin-Titan/titan/node/modules/dtypes"
)

type Project struct {
	ID        string // Id
	Name      string
	Status    ProjectReplicaStatus
	BundleURL string
	Port      int

	Msg string
}

type ProjectType int64

const (
	ProjectTypeTunnel ProjectType = iota
)

type ProjectReplicaStatus int

const (
	ProjectReplicaStatusStarting ProjectReplicaStatus = iota
	ProjectReplicaStatusStarted
	ProjectReplicaStatusError
	ProjectReplicaStatusOffline
)

// String status to string
func (ps ProjectReplicaStatus) String() string {
	switch ps {
	case ProjectReplicaStatusStarting:
		return "starting"
	case ProjectReplicaStatusStarted:
		return "started"
	case ProjectReplicaStatusError:
		return "error"
	case ProjectReplicaStatusOffline:
		return "offline"
	default:
		return "invalidStatus"
	}
}

type ProjectReq struct {
	UUID   string
	NodeID string
	UserID string

	Name      string
	BundleURL string
	Replicas  int64
}

type DeployProjectReq struct {
	UUID       string
	Name       string
	BundleURL  string
	UserID     string
	Replicas   int64
	Expiration time.Time
	Type       ProjectType

	Requirement ProjectRequirement
}

type ProjectRequirement struct {
	CPUCores int64
	Memory   int64
	AreaID   string
	Version  int64

	NodeIDs []string
}

type ProjectInfo struct {
	// uuid
	UUID        string          `db:"id"`
	State       string          `db:"state"`
	Name        string          `db:"name"`
	BundleURL   string          `db:"bundle_url"`
	Replicas    int64           `db:"replicas"`
	ServerID    dtypes.ServerID `db:"scheduler_sid"`
	Expiration  time.Time       `db:"expiration"`
	CreatedTime time.Time       `db:"created_time"`
	UserID      string          `db:"user_id"`
	Type        ProjectType     `db:"type"`

	RequirementByte []byte `db:"requirement"`
	Requirement     ProjectRequirement

	DetailsList       []*ProjectReplicas
	RetryCount        int64 `db:"retry_count"`
	ReplenishReplicas int64 `db:"replenish_replicas"`
}

type ProjectReplicas struct {
	ID            string               `db:"id"`
	Status        ProjectReplicaStatus `db:"status"`
	NodeID        string               `db:"node_id"`
	CreatedTime   time.Time            `db:"created_time"`
	EndTime       time.Time            `db:"end_time"`
	Type          ProjectType          `db:"type"`
	UploadTraffic int64                `db:"upload_traffic"`
	DownTraffic   int64                `db:"download_traffic"`
	Time          int64                `db:"time"`
	MaxTimeout    int64                `db:"max_timeout"`
	MinTimeout    int64                `db:"min_timeout"`

	WsURL     string
	BundleURL string
	IP        string
	GeoID     string
}

// ProjectStateInfo represents information about an project state
type ProjectStateInfo struct {
	ID                string `db:"id"`
	State             string `db:"state"`
	RetryCount        int64  `db:"retry_count"`
	ReplenishReplicas int64  `db:"replenish_replicas"`
}

type ProjectEvent int

const (
	ProjectEventRemove ProjectEvent = iota
	ProjectEventAdd
	ProjectEventNodeOffline
	ProjectEventExpiration
	ProjectEventFailed
)

// ProjectOverview
type ProjectOverview struct {
	NodeID          string `db:"node_id"`
	UploadTraffic   int64  `db:"sum_upload_traffic"`
	DownloadTraffic int64  `db:"sum_download_traffic"`
	Time            int64  `db:"sum_time"`
	MaxTimeout      int64  `db:"avg_max_timeout"`
	MinTimeout      int64  `db:"avg_min_timeout"`
}

// ListProjectOverviewRsp list replica events
type ListProjectOverviewRsp struct {
	Total int                `json:"total"`
	List  []*ProjectOverview `json:"list"`
}

// ListProjectReplicaRsp list replica info
type ListProjectReplicaRsp struct {
	Total int                `json:"total"`
	List  []*ProjectReplicas `json:"list"`
}
