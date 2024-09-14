package db

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/Filecoin-Titan/titan/node/modules/dtypes"
	"github.com/jmoiron/sqlx"

	logging "github.com/ipfs/go-log/v2"
)

var log = logging.Logger("db")

// SQLDB represents a scheduler sql database.
type SQLDB struct {
	db *sqlx.DB
}

// NewSQLDB creates a new SQLDB instance.
func NewSQLDB(db *sqlx.DB) (*SQLDB, error) {
	s := &SQLDB{db}

	return s, nil
}

const (
	// remove table
	retrieveEventTable = "retrieve_event"

	// Database table names.
	nodeRegisterTable  = "node_register_info"
	nodeInfoTable      = "node_info"
	onlineCountTable   = "online_count"
	candidateCodeTable = "candidate_code"

	assetRecordTable   = "asset_record"
	replicaInfoTable   = "replica_info"
	assetsViewTable    = "asset_view"
	bucketTable        = "bucket"
	assetDownloadTable = "asset_download"

	projectEventTable     = "project_event"
	validationResultTable = "validation_result"
	replicaEventTable     = "replica_event"

	edgeUpdateTable      = "edge_update_info"
	validatorsTable      = "validators"
	workloadRecordTable  = "workload_record"
	replenishBackupTable = "replenish_backup"
	awsDataTable         = "aws_data"
	profitDetailsTable   = "profit_details"
	projectInfoTable     = "project_info"
	projectReplicasTable = "project_replicas"

	deploymentTable = "deployments"
	providersTable  = "providers"
	propertiesTable = "properties"
	servicesTable   = "services"
	domainsTable    = "domains"

	nodeStatisticsTable = "node_statistics"
	nodeRetrieveTable   = "node_retrieve"

	// Default limits for loading table entries.
	loadNodeInfosDefaultLimit           = 1000
	loadValidationResultsDefaultLimit   = 100
	loadAssetRecordsDefaultLimit        = 1000
	loadExpiredAssetRecordsDefaultLimit = 100
	loadWorkloadDefaultLimit            = 100
	loadReplicaEventDefaultLimit        = 500
	loadRetrieveDefaultLimit            = 100
	loadReplicaDefaultLimit             = 100
	loadAssetDownloadLimit              = 500
)

// assetStateTable returns the asset state table name for the given serverID.
func assetStateTable(serverID dtypes.ServerID) string {
	str := strings.ReplaceAll(string(serverID), "-", "")
	return fmt.Sprintf("asset_state_%s", str)
}

// projectStateTable returns the project state table name for the given serverID.
func projectStateTable(serverID dtypes.ServerID) string {
	str := strings.ReplaceAll(string(serverID), "-", "")
	return fmt.Sprintf("project_state_%s", str)
}

// InitTables initializes data tables.
func InitTables(d *SQLDB, serverID dtypes.ServerID) error {
	doExec(d, serverID)

	// init table
	tx, err := d.db.Beginx()
	if err != nil {
		return err
	}

	defer func() {
		err = tx.Rollback()
		if err != nil && err != sql.ErrTxDone {
			log.Errorf("InitTables Rollback err:%s", err.Error())
		}

		// doExec2(d)
	}()

	// Execute table creation statements
	tx.MustExec(fmt.Sprintf(cAssetStateTable, assetStateTable(serverID)))
	tx.MustExec(fmt.Sprintf(cReplicaInfoTable, replicaInfoTable))
	tx.MustExec(fmt.Sprintf(cNodeInfoTable, nodeInfoTable))
	tx.MustExec(fmt.Sprintf(cValidationResultsTable, validationResultTable))
	tx.MustExec(fmt.Sprintf(cNodeRegisterTable, nodeRegisterTable))
	tx.MustExec(fmt.Sprintf(cAssetRecordTable, assetRecordTable))
	tx.MustExec(fmt.Sprintf(cEdgeUpdateTable, edgeUpdateTable))
	tx.MustExec(fmt.Sprintf(cValidatorsTable, validatorsTable))
	tx.MustExec(fmt.Sprintf(cAssetViewTable, assetsViewTable))
	tx.MustExec(fmt.Sprintf(cBucketTable, bucketTable))
	tx.MustExec(fmt.Sprintf(cWorkloadTable, workloadRecordTable))
	tx.MustExec(fmt.Sprintf(cReplicaEventTable, replicaEventTable))
	tx.MustExec(fmt.Sprintf(cRetrieveEventTable, retrieveEventTable))
	tx.MustExec(fmt.Sprintf(cReplenishBackupTable, replenishBackupTable))
	tx.MustExec(fmt.Sprintf(cAWSDataTable, awsDataTable))
	tx.MustExec(fmt.Sprintf(cProfitDetailsTable, profitDetailsTable))
	tx.MustExec(fmt.Sprintf(cCandidateCodeTable, candidateCodeTable))
	tx.MustExec(fmt.Sprintf(cProjectStateTable, projectStateTable(serverID)))
	tx.MustExec(fmt.Sprintf(cProjectInfosTable, projectInfoTable))
	tx.MustExec(fmt.Sprintf(cProjectReplicasTable, projectReplicasTable))
	tx.MustExec(fmt.Sprintf(cProjectEventTable, projectEventTable))
	tx.MustExec(fmt.Sprintf(cOnlineCountTable, onlineCountTable))
	tx.MustExec(fmt.Sprintf(cDeploymentTable, deploymentTable))
	tx.MustExec(fmt.Sprintf(cProviderTable, providersTable))
	tx.MustExec(fmt.Sprintf(cPropertiesTable, propertiesTable))
	tx.MustExec(fmt.Sprintf(cServicesTable, servicesTable))
	tx.MustExec(fmt.Sprintf(cDomainTable, domainsTable))
	tx.MustExec(fmt.Sprintf(cAssetDownloadTable, assetDownloadTable))

	tx.MustExec(fmt.Sprintf(cNodeStatisticsTable, nodeStatisticsTable))
	tx.MustExec(fmt.Sprintf(cNodeRetrieveTable, nodeRetrieveTable))

	return tx.Commit()
}

func doExec(d *SQLDB, serverID dtypes.ServerID) {
	// _, err := d.db.Exec(fmt.Sprintf("ALTER TABLE %s CHANGE area_id area_id       VARCHAR(256)   DEFAULT ''", onlineCountTable))
	// if err != nil {
	// 	log.Errorf("InitTables doExec err:%s", err.Error())
	// }
	// _, err = d.db.Exec(fmt.Sprintf("ALTER TABLE %s DROP COLUMN cpu_cores ;", projectInfoTable))
	// if err != nil {
	// 	log.Errorf("InitTables doExec err:%s", err.Error())
	// }

	// --------------------------------------
	// _, err := d.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD owner VARCHAR(128) DEFAULT ''", assetRecordTable))
	// if err != nil {
	// 	log.Errorf("InitTables doExec err:%s", err.Error())
	// }
	// _, err = d.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD user_id         VARCHAR(128)  DEFAULT ''", assetDownloadTable))
	// if err != nil {
	// 	log.Errorf("InitTables doExec err:%s", err.Error())
	// }
	// _, err = d.db.Exec(fmt.Sprintf("DROP TABLE %s ", replicaEventTable))
	// if err != nil {
	// 	log.Errorf("InitTables doExec err:%s", err.Error())
	// }
	// _, err := d.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD total_size    BIGINT       DEFAULT 0", replicaInfoTable))
	// if err != nil {
	// 	log.Errorf("InitTables doExec err:%s", err.Error())
	// }
	// _, err = d.db.Exec(fmt.Sprintf("ALTER TABLE %s speed         INT          DEFAULT 0", replicaInfoTable))
	// if err != nil {
	// 	log.Errorf("InitTables doExec err:%s", err.Error())
	// }
	// _, err = d.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD client_id     VARCHAR(128) DEFAULT ''", replicaInfoTable))
	// if err != nil {
	// 	log.Errorf("InitTables doExec err:%s", err.Error())
	// }
	_, err := d.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD done_size     BIGINT       DEFAULT 0", replicaEventTable))
	if err != nil {
		log.Errorf("InitTables doExec err:%s", err.Error())
	}
	_, err = d.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD type          TINYINT        DEFAULT 0", projectInfoTable))
	if err != nil {
		log.Errorf("InitTables doExec err:%s", err.Error())
	}

	_, err = d.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD upload_traffic     BIGINT        DEFAULT 0", projectReplicasTable))
	if err != nil {
		log.Errorf("InitTables doExec err:%s", err.Error())
	}
	_, err = d.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD download_traffic   BIGINT        DEFAULT 0", projectReplicasTable))
	if err != nil {
		log.Errorf("InitTables doExec err:%s", err.Error())
	}
	_, err = d.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD type          TINYINT        DEFAULT 0", projectReplicasTable))
	if err != nil {
		log.Errorf("InitTables doExec err:%s", err.Error())
	}
	_, err = d.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD time               INT           DEFAULT 0", projectReplicasTable))
	if err != nil {
		log.Errorf("InitTables doExec err:%s", err.Error())
	}
	_, err = d.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD max_timeout        INT           DEFAULT 0", projectReplicasTable))
	if err != nil {
		log.Errorf("InitTables doExec err:%s", err.Error())
	}
	_, err = d.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD min_timeout        INT           DEFAULT 0", projectReplicasTable))
	if err != nil {
		log.Errorf("InitTables doExec err:%s", err.Error())
	}
}

func doExec2(d *SQLDB) {
	_, err := d.db.Exec(`INSERT INTO node_statistics (node_id, asset_count, retrieve_count)
SELECT node_id, asset_count, retrieve_count FROM node_info
ON DUPLICATE KEY UPDATE
    asset_count = VALUES(asset_count),
    retrieve_count = VALUES(retrieve_count);`)
	if err != nil {
		log.Errorf("InitTables doExec err:%s", err.Error())
		return
	}

	_, err = d.db.Exec(fmt.Sprintf("ALTER TABLE %s DROP COLUMN asset_count ;", nodeInfoTable))
	if err != nil {
		log.Errorf("InitTables doExec err:%s", err.Error())
	}

	_, err = d.db.Exec(fmt.Sprintf("ALTER TABLE %s DROP COLUMN retrieve_count ;", nodeInfoTable))
	if err != nil {
		log.Errorf("InitTables doExec err:%s", err.Error())
	}
}
