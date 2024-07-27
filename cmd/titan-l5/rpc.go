package main

import (
	"context"
	"net/http"

	"github.com/Filecoin-Titan/titan/api"
	"github.com/Filecoin-Titan/titan/api/types"
	"github.com/Filecoin-Titan/titan/lib/rpcenc"
	"github.com/Filecoin-Titan/titan/metrics/proxy"
	"github.com/Filecoin-Titan/titan/node/handler"
	"github.com/filecoin-project/go-jsonrpc"
	"github.com/gorilla/mux"
)

func l5Handler(verify func(ctx context.Context, token string) (*types.JWTPayload, error), a api.L5, permissioned bool) http.Handler {
	mux := mux.NewRouter()
	readerHandler, readerServerOpt := rpcenc.ReaderParamDecoder()
	rpcServer := jsonrpc.NewServer(readerServerOpt)

	wapi := proxy.MetricedL5API(a)
	if permissioned {
		wapi = api.PermissionedL5API(wapi)
	}

	rpcServer.Register("titan", wapi)

	mux.Handle("/rpc/v0", rpcServer)
	mux.Handle("/rpc/streams/v0/push/{uuid}", readerHandler)
	mux.PathPrefix("/").Handler(http.DefaultServeMux) // pprof

	if !permissioned {
		return mux
	}

	return handler.New(verify, mux.ServeHTTP)
}
