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
	NodeID      string      `db:"node_id"`
	TaskID      string      `db:"task_id"`
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

// ReplicaEventInfo replica event info
type ReplicaEventInfo struct {
	NodeID      string       `db:"node_id"`
	Event       ReplicaEvent `db:"event"`
	Hash        string       `db:"hash"`
	CreatedTime time.Time    `db:"created_time"`
	Source      AssetSource  `db:"source"`
	TaskID      string       `db:"task_id"`
	ClientID    string       `db:"client_id"`
	Speed       int64        `db:"speed"`

	Cid       string `db:"cid"`
	TotalSize int64  `db:"total_size"`
}

// ListReplicaEventRsp list replica events
type ListReplicaEventRsp struct {
	Total         int                 `json:"total"`
	ReplicaEvents []*ReplicaEventInfo `json:"replica_events"`
}
