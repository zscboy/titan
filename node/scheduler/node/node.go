package node

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Filecoin-Titan/titan/api"
	"github.com/Filecoin-Titan/titan/api/client"
	"github.com/Filecoin-Titan/titan/api/types"
	titanrsa "github.com/Filecoin-Titan/titan/node/rsa"
	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-jsonrpc"
)

// Node represents an Edge or Candidate node
type Node struct {
	// NodeID string
	*API
	jsonrpc.ClientCloser
	*types.NodeInfo
	token                      string
	lastRequestTime            time.Time // Node last keepalive time
	selectWeights              []int     // The select weights assigned by the scheduler to each online node
	numberOfIPChanges          int64
	resetNumberOfIPChangesTime time.Time

	// node info
	PublicKey             *rsa.PublicKey
	RemoteAddr            string
	TCPPort               int
	ExternalURL           string
	IsPrivateMinioOnly    bool
	IsStorageOnly         bool
	IncomeIncr            float64
	BackProjectTime       int64
	MeetCandidateStandard bool

	// IsPhone               bool
	// NATType            types.NatType
	// CPUUsage           float64
	// DiskUsage          float64
	// TitanDiskUsage     float64
	// Type               types.NodeType
	// PortMapping        string
	// BandwidthDown      int64
	// BandwidthUp        int64
	// NetFlowUp          int64
	// NetFlowDown        int64
	// DownloadTraffic    int64
	// UploadTraffic      int64
	// WSServerID         string
	// ExternalIP         string
	// DiskSpace          float64
	// AvailableDiskSpace float64
	// IsTestNode         bool
	// DeactivateTime     int64
}

// API represents the node API
type API struct {
	// common api
	api.Common
	api.Device
	api.Validation
	api.DataSync
	api.Asset
	api.Workerd
	WaitQuiet func(ctx context.Context) error
	// edge api
	// ExternalServiceAddress func(ctx context.Context, candidateURL string) (string, error)
	UserNATPunch func(ctx context.Context, sourceURL string, req *types.NatPunchReq) error
	// candidate api
	GetBlocksOfAsset        func(ctx context.Context, assetCID string, randomSeed int64, randomCount int) ([]string, error)
	CheckNetworkConnectable func(ctx context.Context, network, targetURL string) (bool, error)
	GetMinioConfig          func(ctx context.Context) (*types.MinioConfig, error)
}

// New creates a new node
func New() *Node {
	node := &Node{
		resetNumberOfIPChangesTime: time.Now(),
	}

	return node
}

// APIFromEdge creates a new API from an Edge API
func APIFromEdge(api api.Edge) *API {
	a := &API{
		Common:       api,
		Device:       api,
		Validation:   api,
		DataSync:     api,
		Asset:        api,
		WaitQuiet:    api.WaitQuiet,
		UserNATPunch: api.UserNATPunch,
		Workerd:      api,
	}
	return a
}

// APIFromCandidate creates a new API from a Candidate API
func APIFromCandidate(api api.Candidate) *API {
	a := &API{
		Common:                  api,
		Device:                  api,
		Validation:              api,
		DataSync:                api,
		Asset:                   api,
		WaitQuiet:               api.WaitQuiet,
		GetBlocksOfAsset:        api.GetBlocksWithAssetCID,
		CheckNetworkConnectable: api.CheckNetworkConnectable,
		GetMinioConfig:          api.GetMinioConfig,
	}
	return a
}

func (n *Node) SetNumberOfIPChanges(count int64) {
	n.numberOfIPChanges = count

	if count == 0 {
		n.resetNumberOfIPChangesTime = time.Now()
	}
}

func (n *Node) GetNumberOfIPChanges() (int64, time.Time) {
	return n.numberOfIPChanges, n.resetNumberOfIPChangesTime
}

// ConnectRPC connects to the node RPC
func (n *Node) ConnectRPC(transport *quic.Transport, addr string, nodeType types.NodeType) error {
	httpClient, err := client.NewHTTP3ClientWithPacketConn(transport)
	if err != nil {
		return err
	}

	rpcURL := fmt.Sprintf("https://%s/rpc/v0", addr)

	headers := http.Header{}
	headers.Add("Authorization", "Bearer "+n.token)

	if nodeType == types.NodeEdge {
		// Connect to node
		edgeAPI, closer, err := client.NewEdge(context.Background(), rpcURL, headers, jsonrpc.WithHTTPClient(httpClient))
		if err != nil {
			return xerrors.Errorf("NewEdge err:%s,url:%s", err.Error(), rpcURL)
		}

		n.API = APIFromEdge(edgeAPI)
		n.ClientCloser = closer
		return nil
	}

	if nodeType == types.NodeCandidate {
		// Connect to node
		candidateAPI, closer, err := client.NewCandidate(context.Background(), rpcURL, headers, jsonrpc.WithHTTPClient(httpClient))
		if err != nil {
			return xerrors.Errorf("NewCandidate err:%s,url:%s", err.Error(), rpcURL)
		}

		n.API = APIFromCandidate(candidateAPI)
		n.ClientCloser = closer
		return nil
	}

	return xerrors.Errorf("node %s type %d not wrongful", n.NodeID, n.Type)
}

// IsAbnormal is node abnormal
func (n *Node) IsAbnormal() bool {
	// waiting for deactivate
	if n.DeactivateTime > 0 {
		return true
	}

	// is minio node
	if n.IsPrivateMinioOnly {
		return true
	}

	return false
}

// SelectWeights get node select weights
func (n *Node) SelectWeights() []int {
	return n.selectWeights
}

// SetToken sets the token of the node
func (n *Node) SetToken(t string) {
	n.token = t
}

// GetToken get the token of the node
func (n *Node) GetToken() string {
	return n.token
}

// TCPAddr returns the tcp address of the node
func (n *Node) TCPAddr() string {
	index := strings.Index(n.RemoteAddr, ":")
	ip := n.RemoteAddr[:index+1]
	return fmt.Sprintf("%s%d", ip, n.TCPPort)
}

// RPCURL returns the rpc url of the node
func (n *Node) RPCURL() string {
	return fmt.Sprintf("https://%s/rpc/v0", n.RemoteAddr)
}

func (n *Node) WsURL() string {
	wsURL, err := transformURL(n.ExternalURL)
	if err != nil {
		wsURL = fmt.Sprintf("ws://%s", n.RemoteAddr)
	}

	return wsURL
}

func transformURL(inputURL string) (string, error) {
	// Parse the URL from the string
	parsedURL, err := url.Parse(inputURL)
	if err != nil {
		return "", err
	}

	switch parsedURL.Scheme {
	case "https":
		parsedURL.Scheme = "wss"
	case "http":
		parsedURL.Scheme = "ws"
	default:
		return "", xerrors.New("Scheme not http or https")
	}

	// Remove the path to clear '/rpc/v0'
	parsedURL.Path = ""

	// Return the modified URL as a string
	return parsedURL.String(), nil
}

// DownloadAddr returns the download address of the node
func (n *Node) DownloadAddr() string {
	addr := n.RemoteAddr
	if n.PortMapping != "" {
		index := strings.Index(n.RemoteAddr, ":")
		ip := n.RemoteAddr[:index+1]
		addr = ip + n.PortMapping
	}

	return addr
}

// LastRequestTime returns the last request time of the node
func (n *Node) LastRequestTime() time.Time {
	return n.lastRequestTime
}

// SetLastRequestTime sets the last request time of the node
func (n *Node) SetLastRequestTime(t time.Time) {
	n.lastRequestTime = t
}

// Token returns the token of the node
func (n *Node) Token(cid, clientID string, titanRsa *titanrsa.Rsa, privateKey *rsa.PrivateKey) (*types.Token, *types.TokenPayload, error) {
	tkPayload := &types.TokenPayload{
		ID:          uuid.NewString(),
		NodeID:      n.NodeID,
		AssetCID:    cid,
		ClientID:    clientID, // TODO auth client and allocate id
		CreatedTime: time.Now(),
		Expiration:  time.Now().Add(10 * time.Hour),
	}

	b, err := n.encryptTokenPayload(tkPayload, n.PublicKey, titanRsa)
	if err != nil {
		return nil, nil, xerrors.Errorf("%s encryptTokenPayload err:%s", n.NodeID, err.Error())
	}

	sign, err := titanRsa.Sign(privateKey, b)
	if err != nil {
		return nil, nil, xerrors.Errorf("%s Sign err:%s", n.NodeID, err.Error())
	}

	return &types.Token{ID: tkPayload.ID, CipherText: hex.EncodeToString(b), Sign: hex.EncodeToString(sign)}, tkPayload, nil
}

// encryptTokenPayload encrypts a token payload object using the given public key and RSA instance.
func (n *Node) encryptTokenPayload(tkPayload *types.TokenPayload, publicKey *rsa.PublicKey, rsa *titanrsa.Rsa) ([]byte, error) {
	var buffer bytes.Buffer
	enc := gob.NewEncoder(&buffer)
	err := enc.Encode(tkPayload)
	if err != nil {
		return nil, err
	}

	return rsa.Encrypt(buffer.Bytes(), publicKey)
}

func bToGB(b float64) float64 {
	return b / 1024 / 1024 / 1024
}

func bToMB(b float64) float64 {
	return b / 1024 / 1024
}

func bToKB(b float64) float64 {
	return b / 1024
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}

	return b
}

func (n *Node) DiskEnough(size float64) bool {
	residual := ((100 - n.DiskUsage) / 100) * n.DiskSpace
	if residual <= size {
		return false
	}

	residual = n.AvailableDiskSpace - n.TitanDiskUsage
	if residual <= size {
		return false
	}

	return true
}

func (n *Node) NetFlowUpExcess(size float64) bool {
	if n.NetFlowUp <= 0 {
		return false
	}

	if n.NetFlowUp >= n.UploadTraffic+int64(size) {
		return false
	}

	return true
}

func (n *Node) NetFlowDownExcess(size float64) bool {
	if n.NetFlowDown <= 0 {
		return false
	}

	if n.NetFlowDown >= n.DownloadTraffic+int64(size) {
		return false
	}

	return true
}
