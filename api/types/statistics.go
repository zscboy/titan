package types

import "time"

// EventStatus
type EventStatus int

const (
	// EventStatusSucceed status
	EventStatusSucceed EventStatus = iota
	// EventStatusFailed status
	EventStatusFailed
)

// RetrieveEvent retrieve event
type RetrieveEvent struct {
	TraceID     string      `db:"trace_id"`
	NodeID      string      `db:"node_id"`
	ClientID    string      `db:"client_id"`
	Hash        string      `db:"hash"`
	Speed       int64       `db:"speed"`
	Size        int64       `db:"size"`
	Status      EventStatus `db:"status"`
	CreatedTime time.Time   `db:"created_time"`

	PeakBandwidth int64
}

// ListRetrieveEventRsp list retrieve event
type ListRetrieveEventRsp struct {
	Total int              `json:"total"`
	List  []*RetrieveEvent `json:"list"`
}

// AssetReplicaEventInfo replica event info
type AssetReplicaEventInfo struct {
	NodeID      string       `db:"node_id"`
	Event       ReplicaEvent `db:"event"`
	Hash        string       `db:"hash"`
	CreatedTime time.Time    `db:"created_time"`
	Source      AssetSource  `db:"source"`
	ClientID    string       `db:"client_id"`
	Speed       int64        `db:"speed"`

	Cid       string `db:"cid"`
	TotalSize int64  `db:"total_size"`
	DoneSize  int64  `db:"done_size"`
	TraceID   string `db:"trace_id"`
	Msg       string `db:"msg"`
}

// ListAssetReplicaEventRsp list replica events
type ListAssetReplicaEventRsp struct {
	Total int                      `json:"total"`
	List  []*AssetReplicaEventInfo `json:"list"`
}

// ProjectReplicaEventInfo replica event info
type ProjectReplicaEventInfo struct {
	NodeID      string       `db:"node_id"`
	Event       ProjectEvent `db:"event"`
	ID          string       `db:"id"`
	CreatedTime time.Time    `db:"created_time"`
}

// ListProjectReplicaEventRsp list replica events
type ListProjectReplicaEventRsp struct {
	Total int                        `json:"total"`
	List  []*ProjectReplicaEventInfo `json:"list"`
}
