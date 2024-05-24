package scheduler

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"database/sql"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/Filecoin-Titan/titan/node/modules/dtypes"
	"github.com/Filecoin-Titan/titan/node/scheduler/nat"
	"github.com/Filecoin-Titan/titan/node/scheduler/validation"
	"github.com/Filecoin-Titan/titan/node/scheduler/workload"
	"github.com/docker/go-units"
	"github.com/quic-go/quic-go"

	"go.uber.org/fx"

	"github.com/Filecoin-Titan/titan/node/config"
	"github.com/Filecoin-Titan/titan/node/scheduler/assets"

	"github.com/Filecoin-Titan/titan/api"
	"github.com/Filecoin-Titan/titan/api/types"
	"github.com/Filecoin-Titan/titan/node/common"
	"github.com/Filecoin-Titan/titan/node/handler"
	"github.com/Filecoin-Titan/titan/node/scheduler/node"
	logging "github.com/ipfs/go-log/v2"

	titanrsa "github.com/Filecoin-Titan/titan/node/rsa"
	sSync "github.com/Filecoin-Titan/titan/node/scheduler/sync"
	"golang.org/x/xerrors"
)

var log = logging.Logger("scheduler")

const (
	cpuLimit           = 140
	memoryLimit        = 2250 * units.GiB
	diskSpaceLimit     = 500 * units.TiB
	bandwidthUpLimit   = 200 * units.MiB
	availableDiskLimit = 2 * units.TiB

	validatorCpuLimit    = 8
	validatorMemoryLimit = 8 * units.GiB
)

// Scheduler represents a scheduler node in a distributed system.
type Scheduler struct {
	fx.In

	*common.CommonAPI
	*EdgeUpdateManager
	dtypes.ServerID

	NodeManager            *node.Manager
	ValidationMgr          *validation.Manager
	AssetManager           *assets.Manager
	NatManager             *nat.Manager
	DataSync               *sSync.DataSync
	SchedulerCfg           *config.SchedulerCfg
	SetSchedulerConfigFunc dtypes.SetSchedulerConfigFunc
	GetSchedulerConfigFunc dtypes.GetSchedulerConfigFunc
	WorkloadManager        *workload.Manager

	PrivateKey *rsa.PrivateKey
	Transport  *quic.Transport
}

var _ api.Scheduler = &Scheduler{}

// nodeConnect processes a node connect request with the given options and node type.
func (s *Scheduler) nodeConnect(ctx context.Context, opts *types.ConnectOptions, nodeType types.NodeType) error {
	remoteAddr := handler.GetRemoteAddr(ctx)
	nodeID := handler.GetNodeID(ctx)

	alreadyConnect := true

	cNode := s.NodeManager.GetNode(nodeID)
	if cNode == nil {
		if err := s.NodeManager.NodeExists(nodeID, nodeType); err != nil {
			return xerrors.Errorf("node: %s, type: %d, error: %w", nodeID, nodeType, err)
		}
		cNode = node.New()
		alreadyConnect = false
	}

	if cNode.ExternalIP != "" {
		s.NodeManager.RemoveNodeIP(nodeID, cNode.ExternalIP)
	}

	externalIP, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return xerrors.Errorf("SplitHostPort err:%s", err.Error())
	}

	if !s.NodeManager.StoreNodeIP(nodeID, externalIP) {
		return xerrors.Errorf("%s The number of IPs exceeds the limit", externalIP)
	}

	defer func() {
		if err != nil {
			s.NodeManager.RemoveNodeIP(nodeID, externalIP)
		}
	}()

	cNode.SetToken(opts.Token)
	cNode.RemoteAddr = remoteAddr
	cNode.ExternalURL = opts.ExternalURL
	cNode.TCPPort = opts.TcpServerPort
	cNode.IsPrivateMinioOnly = opts.IsPrivateMinioOnly

	if len(s.SchedulerCfg.StorageCandidates) > 0 {
		for _, nID := range s.SchedulerCfg.StorageCandidates {
			if nID == nodeID {
				cNode.IsStorageOnly = true
				break
			}
		}
	}

	if len(s.SchedulerCfg.TestCandidates) > 0 {
		_, exist := s.SchedulerCfg.TestCandidates[nodeID]
		if exist {
			cNode.IsTestNode = true
		}
	}

	log.Infof("node connected %s, address:%s , %v", nodeID, remoteAddr, alreadyConnect)

	err = cNode.ConnectRPC(s.Transport, remoteAddr, nodeType)
	if err != nil {
		return xerrors.Errorf("%s nodeConnect ConnectRPC err:%s", nodeID, err.Error())
	}

	// init node info
	nodeInfo, err := cNode.API.GetNodeInfo(context.Background())
	if err != nil {
		log.Errorf("%s nodeConnect NodeInfo err:%s", nodeID, err.Error())
		return err
	}

	if nodeID != nodeInfo.NodeID {
		return xerrors.Errorf("nodeID mismatch %s, %s", nodeID, nodeInfo.NodeID)
	}

	nodeInfo.SchedulerID = s.ServerID

	if nodeInfo.AvailableDiskSpace <= 0 {
		nodeInfo.AvailableDiskSpace = 2 * units.GiB
	}

	if nodeInfo.AvailableDiskSpace > nodeInfo.DiskSpace {
		nodeInfo.AvailableDiskSpace = nodeInfo.DiskSpace
	}

	if nodeType == types.NodeCandidate {
		cNode.MeetCandidateStandard = cNode.IsTestNode || (nodeInfo.Memory > validatorMemoryLimit && nodeInfo.CPUCores > validatorCpuLimit)

		isValidator, err := s.db.IsValidator(nodeID)
		if err != nil {
			return xerrors.Errorf("nodeConnect %s IsValidator err:%s", nodeID, err.Error())
		}

		if isValidator && cNode.MeetCandidateStandard {
			nodeType = types.NodeValidator
		}
		nodeInfo.AvailableDiskSpace = nodeInfo.DiskSpace * 0.9
	} else {
		if nodeInfo.AvailableDiskSpace > availableDiskLimit {
			nodeInfo.AvailableDiskSpace = availableDiskLimit
		}

		err = checkNodeParameters(&nodeInfo)
		if err != nil {
			return err
		}
	}

	nodeInfo.Type = nodeType
	nodeInfo.ExternalIP = externalIP
	nodeInfo.BandwidthUp = units.KiB

	oldInfo, err := s.NodeManager.LoadNodeInfo(nodeID)
	if err != nil && err != sql.ErrNoRows {
		return xerrors.Errorf("load node online duration %s err : %s", nodeID, err.Error())
	}

	size, err := s.db.LoadReplicaSizeByNodeID(nodeID)
	if err != nil {
		return xerrors.Errorf("LoadReplicaSizeByNodeID %s err:%s", nodeID, err.Error())
	}

	cNode.IsPhone = isPhone(nodeInfo.SystemVersion, nodeInfo.CPUInfo, s.SchedulerCfg.AndroidSymbol, s.SchedulerCfg.IOSSymbol)
	// limit node availableDiskSpace to 5 GiB when using phone
	if cNode.IsPhone {
		if nodeInfo.AvailableDiskSpace > float64(5*units.GiB) {
			nodeInfo.AvailableDiskSpace = float64(5 * units.GiB)
		}

		if size > 5*units.GiB {
			size = 5 * units.GiB
		}
	}

	nodeInfo.TitanDiskUsage = float64(size)
	if nodeInfo.AvailableDiskSpace < float64(size) {
		nodeInfo.AvailableDiskSpace = float64(roundUpToNextGB(size))
	}

	if oldInfo != nil {
		// init node info
		nodeInfo.PortMapping = oldInfo.PortMapping
		nodeInfo.OnlineDuration = oldInfo.OnlineDuration
		nodeInfo.BandwidthDown = oldInfo.BandwidthDown
		nodeInfo.BandwidthUp = oldInfo.BandwidthUp
		nodeInfo.DeactivateTime = oldInfo.DeactivateTime
		nodeInfo.DownloadTraffic = oldInfo.DownloadTraffic
		nodeInfo.UploadTraffic = oldInfo.UploadTraffic

		if oldInfo.DeactivateTime > 0 && oldInfo.DeactivateTime < time.Now().Unix() {
			return xerrors.Errorf("The node %s has been deactivate and cannot be logged in", nodeID)
		}
	}

	cNode.BandwidthDown = nodeInfo.BandwidthDown
	cNode.BandwidthUp = nodeInfo.BandwidthUp
	cNode.PortMapping = nodeInfo.PortMapping
	cNode.DeactivateTime = nodeInfo.DeactivateTime
	cNode.AvailableDiskSpace = nodeInfo.AvailableDiskSpace
	cNode.NodeID = nodeInfo.NodeID
	cNode.Type = nodeInfo.Type
	cNode.ExternalIP = nodeInfo.ExternalIP
	cNode.DiskSpace = nodeInfo.DiskSpace
	cNode.TitanDiskUsage = nodeInfo.TitanDiskUsage
	cNode.DiskUsage = nodeInfo.DiskUsage
	cNode.IncomeIncr = (s.NodeManager.NodeCalculateMCx(cNode.IsPhone) * 360)
	cNode.NetFlowUp = nodeInfo.NetFlowUp
	cNode.NetFlowDown = nodeInfo.NetFlowDown
	cNode.DownloadTraffic = nodeInfo.DownloadTraffic
	cNode.UploadTraffic = nodeInfo.UploadTraffic

	// limit node availableDiskSpace to 5 GiB when using phone
	if cNode.IsPhone && nodeInfo.AvailableDiskSpace > float64(5*units.GiB) {
		cNode.AvailableDiskSpace = float64(5 * units.GiB)
	}

	if !alreadyConnect {
		version, err := cNode.API.Version(ctx)
		if err != nil {
			log.Infof("node connected %s, Version:%s ", nodeID, err.Error())
		} else {
			cNode.IsNewVersion = version.Version == "0.1.18"
			log.Infof("node connected %s, Version:%s , %v", nodeID, version.Version, cNode.IsNewVersion)
		}

		pStr, err := s.NodeManager.LoadNodePublicKey(nodeID)
		if err != nil && err != sql.ErrNoRows {
			return xerrors.Errorf("load node port %s err : %s", nodeID, err.Error())
		}

		publicKey, err := titanrsa.Pem2PublicKey([]byte(pStr))
		if err != nil {
			return xerrors.Errorf("load node port %s err : %s", nodeID, err.Error())
		}
		cNode.PublicKey = publicKey

		err = s.NodeManager.NodeOnline(cNode, &nodeInfo)
		if err != nil {
			log.Errorf("nodeConnect err:%s,nodeID:%s", err.Error(), nodeID)
			return err
		}
	}

	if nodeType == types.NodeEdge {
		go s.NatManager.DetermineEdgeNATType(context.Background(), nodeID)
	} else {
		go s.NatManager.DetermineCandidateNATType(context.Background(), nodeID)
	}

	s.DataSync.AddNodeToList(nodeID)
	return nil
}

func isPhone(systemVersion, cpuInfo, androidSymbol, iosSymbol string) bool {
	if strings.Contains(systemVersion, androidSymbol) || strings.Contains(systemVersion, iosSymbol) {
		return true
	}

	notAllowedCPU := strings.Contains(cpuInfo, "Intel") || strings.Contains(cpuInfo, "AMD") || strings.Contains(cpuInfo, "Apple")
	if systemVersion == "0.1.16+api1.0.0" && !notAllowedCPU {
		return true
	}

	return false
}

func roundUpToNextGB(bytes int) int {
	const GB = 1 << 30
	if bytes%GB == 0 {
		return bytes
	}
	return ((bytes / GB) + 1) * GB
}

func checkNodeParameters(nodeInfo *types.NodeInfo) error {
	if nodeInfo.DiskSpace > diskSpaceLimit || nodeInfo.DiskSpace < 0 {
		return xerrors.Errorf("checkNodeParameters [%s] DiskSpace [%.2f]", nodeInfo.NodeID, nodeInfo.DiskSpace)
	}

	if nodeInfo.BandwidthDown < 0 {
		return xerrors.Errorf("checkNodeParameters [%s] BandwidthDown [%d]", nodeInfo.NodeID, nodeInfo.BandwidthDown)
	}

	if nodeInfo.BandwidthUp > bandwidthUpLimit || nodeInfo.BandwidthUp < 0 {
		return xerrors.Errorf("checkNodeParameters [%s] BandwidthUp [%d]", nodeInfo.NodeID, nodeInfo.BandwidthUp)
	}

	if nodeInfo.Memory > memoryLimit || nodeInfo.Memory < 0 {
		return xerrors.Errorf("checkNodeParameters [%s] Memory [%.2f]", nodeInfo.NodeID, nodeInfo.Memory)
	}

	if nodeInfo.CPUCores > cpuLimit || nodeInfo.CPUCores < 0 {
		return xerrors.Errorf("checkNodeParameters [%s] CPUCores [%d]", nodeInfo.NodeID, nodeInfo.CPUCores)
	}

	return nil
}

// NodeValidationResult processes the validation result for a node
func (s *Scheduler) NodeValidationResult(ctx context.Context, r io.Reader, sign string) error {
	validator := handler.GetNodeID(ctx)
	node := s.NodeManager.GetNode(validator)
	if node == nil {
		return fmt.Errorf("node %s not online", validator)
	}

	signBuf, err := hex.DecodeString(sign)
	if err != nil {
		return err
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	rsa := titanrsa.New(crypto.SHA256, crypto.SHA256.New())
	err = rsa.VerifySign(node.PublicKey, signBuf, data)
	if err != nil {
		return err
	}

	result := &api.ValidationResult{}
	buffer := bytes.NewBuffer(data)
	dec := gob.NewDecoder(buffer)
	err = dec.Decode(result)
	if err != nil {
		return err
	}

	result.Validator = validator
	s.ValidationMgr.PushResult(result)

	return nil
}

// TriggerElection triggers a single election for validators.
func (s *Scheduler) TriggerElection(ctx context.Context) error {
	s.ValidationMgr.StartElection()
	return nil
}

// GetValidationResults retrieves a list of validation results.
func (s *Scheduler) GetValidationResults(ctx context.Context, nodeID string, limit, offset int) (*types.ListValidationResultRsp, error) {
	svm, err := s.NodeManager.LoadValidationResultInfos(nodeID, limit, offset)
	if err != nil {
		return nil, err
	}

	return svm, nil
}

// GetSchedulerPublicKey get server publicKey
func (s *Scheduler) GetSchedulerPublicKey(ctx context.Context) (string, error) {
	if s.PrivateKey == nil {
		return "", fmt.Errorf("scheduler private key not exist")
	}

	publicKey := s.PrivateKey.PublicKey
	pem := titanrsa.PublicKey2Pem(&publicKey)
	return string(pem), nil
}

// GetNodePublicKey get node publicKey
func (s *Scheduler) GetNodePublicKey(ctx context.Context, nodeID string) (string, error) {
	pem, err := s.NodeManager.LoadNodePublicKey(nodeID)
	if err != nil {
		return "", xerrors.Errorf("%s load node public key failed: %w", nodeID, err)
	}

	return string(pem), nil
}

// GetValidationInfo  get information related to validation and election
func (s *Scheduler) GetValidationInfo(ctx context.Context) (*types.ValidationInfo, error) {
	eTime := s.ValidationMgr.GetNextElectionTime()

	return &types.ValidationInfo{
		NextElectionTime: eTime,
	}, nil
}

// SubmitUserWorkloadReport submits report of workload for User Asset Download
func (s *Scheduler) SubmitUserWorkloadReport(ctx context.Context, r io.Reader) error {
	// nodeID := handler.GetNodeID(ctx)
	// cipherText, err := io.ReadAll(r)
	// if err != nil {
	// 	return err
	// }

	// log.Infof("SubmitUserWorkloadReport %s , size:%d\n", nodeID, len(cipherText))
	return nil
	// node := s.NodeManager.GetNode(nodeID)

	// if err != nil {
	// 	return err
	// }

	// titanRsa := titanrsa.New(crypto.SHA256, crypto.SHA256.New())
	// data, err := titanRsa.Decrypt(cipherText, s.PrivateKey)
	// if err != nil {
	// 	return xerrors.Errorf("decrypt error: %w", err)
	// }

	// return s.WorkloadManager.HandleUserWorkload(data, node)
}

// SubmitNodeWorkloadReport submits report of workload for node Asset Download
func (s *Scheduler) SubmitNodeWorkloadReport(ctx context.Context, r io.Reader) error {
	// nodeID := handler.GetNodeID(ctx)
	// cipherText, err := io.ReadAll(r)
	// if err != nil {
	// 	return err
	// }

	// log.Infof("SubmitNodeWorkloadReport %s , size:%d\n", nodeID, len(cipherText))
	return nil
	// nodeID := handler.GetNodeID(ctx)
	// node := s.NodeManager.GetNode(nodeID)
	// if node == nil {
	// 	return xerrors.Errorf("node %s not exists", nodeID)
	// }

	// report := &types.NodeWorkloadReport{}
	// dec := gob.NewDecoder(r)
	// err := dec.Decode(report)
	// if err != nil {
	// 	return xerrors.Errorf("decode data to NodeWorkloadReport error: %w", err)
	// }

	// titanRsa := titanrsa.New(crypto.SHA256, crypto.SHA256.New())
	// if err = titanRsa.VerifySign(node.PublicKey, report.Sign, report.CipherText); err != nil {
	// 	return xerrors.Errorf("verify sign error: %w", err)
	// }

	// data, err := titanRsa.Decrypt(report.CipherText, s.PrivateKey)
	// if err != nil {
	// 	return xerrors.Errorf("decrypt error: %w", err)
	// }

	// return s.WorkloadManager.PushResult(data, node)
}

func (s *Scheduler) SubmitWorkloadReportV2(ctx context.Context, workload *types.WorkloadRecordReq) error {
	return s.WorkloadManager.PushResult(workload, "")
}

// SubmitWorkloadReport
func (s *Scheduler) SubmitWorkloadReport(ctx context.Context, workload *types.WorkloadRecordReq) error {
	nodeID := handler.GetNodeID(ctx)

	node := s.NodeManager.GetNode(nodeID)
	if node == nil {
		return xerrors.Errorf("node %s not exists", nodeID)
	}

	return s.WorkloadManager.PushResult(workload, nodeID)
}

// GetWorkloadRecords retrieves a list of workload results.
func (s *Scheduler) GetWorkloadRecords(ctx context.Context, nodeID string, limit, offset int) (*types.ListWorkloadRecordRsp, error) {
	return s.NodeManager.LoadWorkloadRecords(nodeID, limit, offset)
}

// GetWorkloadRecord retrieves a list of workload results.
func (s *Scheduler) GetWorkloadRecord(ctx context.Context, id string) (*types.WorkloadRecord, error) {
	return s.NodeManager.LoadWorkloadRecordOfID(id)
}

// GetRetrieveEventRecords retrieves a list of retrieve events
func (s *Scheduler) GetRetrieveEventRecords(ctx context.Context, nodeID string, limit, offset int) (*types.ListRetrieveEventRsp, error) {
	return s.NodeManager.LoadRetrieveEventRecords(nodeID, limit, offset)
}

// // GetWorkloadRecord retrieves workload result.
// func (s *Scheduler) GetWorkloadRecord(ctx context.Context, tokenID string) (*types.WorkloadRecord, error) {
// 	return s.NodeManager.LoadWorkloadRecord(tokenID)
// }

// ElectValidators elect validators
func (s *Scheduler) ElectValidators(ctx context.Context, nodeIDs []string, cleanOld bool) error {
	if len(nodeIDs) == 0 {
		return nil
	}

	return s.ValidationMgr.CompulsoryElection(nodeIDs, cleanOld)
}

func (s *Scheduler) AddProfits(ctx context.Context, nodes []string, profit float64) error {
	return s.db.UpdateNodeProfit(nodes, profit)
}

// UpdateNetFlows update node net flow total,up,down usage
func (s *Scheduler) UpdateNetFlows(ctx context.Context, total, up, down int64) error {
	return nil
}

func (s *Scheduler) ReDetermineNodeNATType(ctx context.Context, nodeID string) error {
	node := s.NodeManager.GetCandidateNode(nodeID)
	if node != nil {
		go s.NatManager.DetermineCandidateNATType(ctx, nodeID)
		return nil
	}

	node = s.NodeManager.GetEdgeNode(nodeID)
	if node != nil {
		go s.NatManager.DetermineCandidateNATType(ctx, nodeID)
		return nil
	}

	return nil
}
