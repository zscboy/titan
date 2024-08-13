package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/Filecoin-Titan/titan/lib/carutil"
	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
)

func (hs *HttpServer) uploadv3Handler(w http.ResponseWriter, r *http.Request) {
	log.Debug("uploadHandler")
	setAccessControlAllowForHeader(w)
	if r.Method == http.MethodOptions {
		return
	}

	if r.Method != http.MethodGet {
		uploadResult(w, -1, fmt.Sprintf("only allow get method, http status code %d", http.StatusMethodNotAllowed))
		return
	}

	userID, _, err := verifyToken(r, hs.apiSecret)
	if err != nil {
		log.Errorf("verfiy token error: %s", err.Error())
		uploadResult(w, -1, fmt.Sprintf("%s, http status code %d", err.Error(), http.StatusUnauthorized))
		return
	}

	sourceAddr := r.URL.Query().Get("url")

	headResp, err := http.Head(sourceAddr)
	if err != nil {
		log.Errorf("http head to %s error: %s", sourceAddr, err.Error())
		uploadResult(w, -1, fmt.Sprintf("%s http status code %d", err.Error(), http.StatusBadRequest))
		return
	}
	defer headResp.Body.Close()
	// ignore 200/405
	if headResp.StatusCode != http.StatusOK && headResp.StatusCode != http.StatusMethodNotAllowed {
		uploadResult(w, -1, fmt.Sprintf("remote source file not available %s, http status code %d", sourceAddr, headResp.StatusCode))
	}

	// limit max concurrent
	semaphore <- struct{}{}
	defer func() { <-semaphore }()

	rootCid, statusCode, err := hs.handleDownload(userID, sourceAddr, int64(hs.maxSizeOfUploadFile))
	if err != nil {
		uploadResult(w, -1, fmt.Sprintf("%s, http status code %d", err.Error(), statusCode))
		return
	}

	type Result struct {
		Code int    `json:"code"`
		Err  int    `json:"err"`
		Msg  string `json:"msg"`
		Cid  string `json:"cid"`
	}

	ret := Result{Code: 0, Err: 0, Msg: "", Cid: rootCid.String()}
	buf, err := json.Marshal(ret)
	if err != nil {
		log.Errorf("marshal error %s", err.Error())
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err = w.Write(buf); err != nil {
		log.Errorf("write error %s", err.Error())
	}
}

func (hs *HttpServer) handleDownload(userId, addr string, maxBytes int64) (cid.Cid, int, error) {
	// Get the uploaded with addr
	parsedUrl, err := url.ParseRequestURI(addr)
	if err != nil {
		log.Errorf("invalid source url: %s", addr)
		return cid.Undef, http.StatusBadRequest, fmt.Errorf("invalid url %s, http status code %d", addr, http.StatusBadRequest)
	}

	resp, err := http.Get(addr)
	if err != nil {
		log.Errorf("http.Get %s error: %s", addr, err.Error())
		return cid.Undef, http.StatusInternalServerError, err
	}
	defer resp.Body.Close()

	fileName := getFileName(resp, parsedUrl)
	log.Debugf("download %s file name: %s", addr, fileName)

	if resp.StatusCode != http.StatusOK {
		log.Errorf("failed to download file from %s, status code: %d", addr, resp.StatusCode)
		return cid.Undef, resp.StatusCode, fmt.Errorf("failed to download file: status code %d", resp.StatusCode)
	}

	limitReader := http.MaxBytesReader(nil, resp.Body, maxBytes)

	assetDir, err := hs.asset.AllocatePathWithSize(resp.ContentLength)
	if err != nil {
		return cid.Cid{}, http.StatusInternalServerError, fmt.Errorf("can not allocate storage for file %s", err.Error())
	}

	assetTempDirPath := path.Join(assetDir, userId+"-"+uuid.NewString())
	if err = os.Mkdir(assetTempDirPath, 0755); err != nil {
		return cid.Cid{}, http.StatusInternalServerError, fmt.Errorf("mkdir fialed %s", err.Error())
	}
	defer os.RemoveAll(assetTempDirPath)

	assetPath := path.Join(assetTempDirPath, fileName)
	out, err := os.Create(assetPath)
	if err != nil {
		return cid.Cid{}, http.StatusInternalServerError, fmt.Errorf("create file failed: %s, path: %s", err.Error(), assetPath)
	}
	defer out.Close()

	if _, err := io.Copy(out, limitReader); err != nil {
		return cid.Cid{}, http.StatusInternalServerError, fmt.Errorf("save file failed: %s, path: %s", err.Error(), assetPath)
	}

	tempCarFile := path.Join(assetDir, uuid.NewString())
	rootCID, err := carutil.CreateCar(assetPath, tempCarFile)
	if err != nil {
		return cid.Cid{}, http.StatusInternalServerError, fmt.Errorf("create car failed: %s, path: %s", err.Error(), tempCarFile)
	}
	defer os.RemoveAll(tempCarFile)

	var isExists bool
	if isExists, err = hs.asset.AssetExists(rootCID); err != nil {
		return cid.Cid{}, http.StatusInternalServerError, err
	} else if isExists {
		log.Debugf("asset %s already exist", rootCID.String())
		return rootCID, http.StatusOK, nil
	}

	if err = hs.saveCarFile(context.Background(), tempCarFile, rootCID); err != nil {
		return cid.Cid{}, http.StatusInternalServerError, fmt.Errorf("save car file failed %s, file %s", err.Error(), fileName)
	}

	return rootCID, http.StatusOK, nil
}

func getFileName(resp *http.Response, url *url.URL) string {
	fileName := getFileNameFromResponse(resp)
	if fileName == "" {
		fileName = getFileNameFromUrl(url)
	}
	return fileName
}

func getFileNameFromResponse(resp *http.Response) string {
	_, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition"))
	if err != nil {
		return ""
	}
	return params["filename"]
}

func getFileNameFromUrl(url *url.URL) string {
	pathSegments := strings.Split(url.Path, "/")
	fileName := pathSegments[len(pathSegments)-1]
	return fileName
}
