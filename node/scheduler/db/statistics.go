package db

import (
	"database/sql"
	"fmt"

	"github.com/Filecoin-Titan/titan/api/types"
	"github.com/jmoiron/sqlx"
)

// SaveRetrieveEventInfo records a retrieval event and updates the associated node information in the database.
func (n *SQLDB) SaveRetrieveEventInfo(info *types.RetrieveEvent, succeededCount, failedCount int) error {
	total := succeededCount + failedCount
	// update node info
	query := fmt.Sprintf(
		`INSERT INTO %s (node_id, retrieve_count, retrieve_succeeded_count, retrieve_failed_count) VALUES (?, ?, ?, ?) 
				ON DUPLICATE KEY UPDATE retrieve_count=retrieve_count+? ,retrieve_succeeded_count=retrieve_succeeded_count+?, retrieve_failed_count=retrieve_failed_count+?, update_time=NOW()`, nodeStatisticsTable)
	_, err := n.db.Exec(query, info.NodeID, total, succeededCount, failedCount, total, succeededCount, failedCount)
	if err != nil {
		return err
	}

	query = fmt.Sprintf(
		`INSERT INTO %s (trace_id, node_id, client_id, hash, size, speed, status ) 
				VALUES (:trace_id, :node_id, :client_id, :hash, :size, :speed, :status )`, nodeRetrieveTable)
	_, err = n.db.NamedExec(query, info)

	return err
}

// SaveReplicaEvent logs a replica event with detailed event information into the database.
func (n *SQLDB) SaveReplicaEvent(info *types.AssetReplicaEventInfo, succeededCount, failedCount int) error {
	tx, err := n.db.Beginx()
	if err != nil {
		return err
	}

	defer func() {
		err = tx.Rollback()
		if err != nil && err != sql.ErrTxDone {
			log.Errorf("SaveReplicaEvent Rollback err:%s", err.Error())
		}
	}()

	total := succeededCount + failedCount

	// update node asset count
	query := fmt.Sprintf(
		`INSERT INTO %s (node_id, asset_count, asset_succeeded_count, asset_failed_count) VALUES (?, ?, ?, ?) 
				ON DUPLICATE KEY UPDATE asset_count=asset_count+? ,asset_succeeded_count=asset_succeeded_count+?, asset_failed_count=asset_failed_count+?, update_time=NOW()`, nodeStatisticsTable)
	_, err = tx.Exec(query, info.NodeID, total, succeededCount, failedCount, total, succeededCount, failedCount)
	if err != nil {
		return err
	}

	// replica event
	err = n.saveReplicaEvent(tx, info)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (n *SQLDB) saveReplicaEvent(tx *sqlx.Tx, info *types.AssetReplicaEventInfo) error {
	qry := fmt.Sprintf(`INSERT INTO %s (node_id, event, hash, source, client_id, speed, cid, total_size, done_size, trace_id, msg) 
		        VALUES (:node_id, :event, :hash, :source, :client_id, :speed, :cid, :total_size, :done_size, :trace_id, :msg)`, replicaEventTable)
	_, err := tx.NamedExec(qry, info)

	return err
}

// SaveProjectEvent logs a replica event with detailed event information into the database.
func (n *SQLDB) SaveProjectEvent(info *types.ProjectReplicaEventInfo, succeededCount, failedCount int) error {
	tx, err := n.db.Beginx()
	if err != nil {
		return err
	}

	defer func() {
		err = tx.Rollback()
		if err != nil && err != sql.ErrTxDone {
			log.Errorf("SaveProjectEvent Rollback err:%s", err.Error())
		}
	}()

	// update node project count
	query := fmt.Sprintf(
		`INSERT INTO %s (node_id, project_count, project_succeeded_count, project_failed_count) VALUES (?, ?, ?, ?) 
				ON DUPLICATE KEY UPDATE project_count=project_count+? ,project_succeeded_count=project_succeeded_count+?, project_failed_count=project_failed_count+?, update_time=NOW()`, nodeStatisticsTable)
	_, err = tx.Exec(query, info.NodeID, 1, succeededCount, failedCount, 1, succeededCount, failedCount)
	if err != nil {
		return err
	}

	// replica event
	err = n.saveProjectReplicaEvent(tx, info)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (n *SQLDB) saveProjectReplicaEvent(tx *sqlx.Tx, info *types.ProjectReplicaEventInfo) error {
	qry := fmt.Sprintf(`INSERT INTO %s (node_id, event, id) 
		        VALUES (:node_id, :event, :id)`, projectEventTable)
	_, err := tx.NamedExec(qry, info)

	return err
}

func (n *SQLDB) LoadNodeStatisticsInfo(nodeID string) (types.NodeStatisticsInfo, error) {
	sInfo := types.NodeStatisticsInfo{}
	query := fmt.Sprintf(`SELECT asset_count,asset_succeeded_count,asset_failed_count,retrieve_count,retrieve_succeeded_count,retrieve_failed_count,
	    project_count,project_succeeded_count,project_failed_count 
	    FROM %s WHERE node_id=?`, nodeStatisticsTable)
	err := n.db.Get(&sInfo, query, nodeID)

	return sInfo, err
}

// LoadReplicaEventCountByStatus retrieves a count of replica for a specific hash filtered by status.
func (n *SQLDB) LoadReplicaEventCountByStatus(hash string, statuses []types.ReplicaEvent) (int, error) {
	sQuery := fmt.Sprintf(`SELECT count(*) FROM %s WHERE hash=? AND event in (?)`, replicaEventTable)
	query, args, err := sqlx.In(sQuery, hash, statuses)
	if err != nil {
		return 0, err
	}

	var out int
	query = n.db.Rebind(query)
	if err := n.db.Get(&out, query, args...); err != nil {
		return 0, err
	}

	return out, nil
}

// LoadReplicaEventsByNode retrieves replica events for a specific node ID, excluding the removal events, with pagination support.
func (n *SQLDB) LoadReplicaEventsByNode(nodeID string, status types.ReplicaEvent, limit, offset int) (*types.ListAssetReplicaEventRsp, error) {
	res := new(types.ListAssetReplicaEventRsp)

	var infos []*types.AssetReplicaEventInfo
	query := fmt.Sprintf("SELECT * FROM %s WHERE node_id=? AND event=? order by created_time desc LIMIT ? OFFSET ? ", replicaEventTable)
	if limit > loadReplicaEventDefaultLimit {
		limit = loadReplicaEventDefaultLimit
	}

	err := n.db.Select(&infos, query, nodeID, status, limit, offset)
	if err != nil {
		return nil, err
	}

	res.List = infos

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE node_id=? AND event=?", replicaEventTable)
	var count int
	err = n.db.Get(&count, countQuery, nodeID, status)
	if err != nil {
		return nil, err
	}

	res.Total = count

	return res, nil
}

// LoadReplicaEventsOfNode retrieves replica events for a specific node ID, excluding the removal events, with pagination support.
func (n *SQLDB) LoadReplicaEventsOfNode(nodeID string, limit, offset int) (*types.ListAssetReplicaEventRsp, error) {
	res := new(types.ListAssetReplicaEventRsp)

	var infos []*types.AssetReplicaEventInfo
	query := fmt.Sprintf("SELECT * FROM %s WHERE node_id=? AND event!=? order by created_time desc LIMIT ? OFFSET ? ", replicaEventTable)
	if limit > loadReplicaEventDefaultLimit {
		limit = loadReplicaEventDefaultLimit
	}

	err := n.db.Select(&infos, query, nodeID, types.ReplicaEventRemove, limit, offset)
	if err != nil {
		return nil, err
	}

	res.List = infos

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE node_id=? AND event!=?", replicaEventTable)
	var count int
	err = n.db.Get(&count, countQuery, nodeID, types.ReplicaEventRemove)
	if err != nil {
		return nil, err
	}

	res.Total = count

	return res, nil
}

// LoadReplicaEventsByHash retrieves replica events for a specific node ID, excluding the removal events, with pagination support.
func (n *SQLDB) LoadReplicaEventsByHash(hash string, status types.ReplicaEvent, limit, offset int) (*types.ListAssetReplicaEventRsp, error) {
	res := new(types.ListAssetReplicaEventRsp)

	var infos []*types.AssetReplicaEventInfo
	query := fmt.Sprintf("SELECT * FROM %s WHERE hash=? AND event=? order by created_time desc LIMIT ? OFFSET ? ", replicaEventTable)
	if limit > loadReplicaEventDefaultLimit {
		limit = loadReplicaEventDefaultLimit
	}

	err := n.db.Select(&infos, query, hash, status, limit, offset)
	if err != nil {
		return nil, err
	}

	res.List = infos

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE hash=? AND event=?", replicaEventTable)
	var count int
	err = n.db.Get(&count, countQuery, hash, status)
	if err != nil {
		return nil, err
	}

	res.Total = count

	return res, nil
}
