// Code generated by protoc-gen-grpc-gateway. DO NOT EDIT.
// source: proto/prysm/v1alpha1/health.proto

/*
Package eth is a reverse proxy.

It translates gRPC into RESTful JSON APIs.
*/
package eth

import (
	"context"
	"github.com/golang/protobuf/ptypes/empty"
	emptypb "github.com/golang/protobuf/ptypes/empty"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/grpc-ecosystem/grpc-gateway/v2/utilities"
	github_com_prysmaticlabs_prysm_v3_consensus_types_primitives "github.com/prysmaticlabs/prysm/v3/consensus-types/primitives"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"io"
	"net/http"
)

// Suppress "imported and not used" errors
var _ codes.Code
var _ io.Reader
var _ status.Status
var _ = runtime.String
var _ = utilities.NewDoubleArray
var _ = metadata.Join
var _ = github_com_prysmaticlabs_prysm_v3_consensus_types_primitives.Epoch(0)
var _ = emptypb.Empty{}
var _ = empty.Empty{}

func request_Health_StreamBeaconLogs_0(ctx context.Context, marshaler runtime.Marshaler, client HealthClient, req *http.Request, pathParams map[string]string) (Health_StreamBeaconLogsClient, runtime.ServerMetadata, error) {
	var protoReq emptypb.Empty
	var metadata runtime.ServerMetadata

	stream, err := client.StreamBeaconLogs(ctx, &protoReq)
	if err != nil {
		return nil, metadata, err
	}
	header, err := stream.Header()
	if err != nil {
		return nil, metadata, err
	}
	metadata.HeaderMD = header
	return stream, metadata, nil

}

// RegisterHealthHandlerServer registers the http handlers for service Health to "mux".
// UnaryRPC     :call HealthServer directly.
// StreamingRPC :currently unsupported pending https://github.com/grpc/grpc-go/issues/906.
// Note that using this registration option will cause many gRPC library features to stop working. Consider using RegisterHealthHandlerFromEndpoint instead.
func RegisterHealthHandlerServer(ctx context.Context, mux *runtime.ServeMux, server HealthServer) error {

	mux.Handle("GET", pattern_Health_StreamBeaconLogs_0, func(w http.ResponseWriter, req *http.Request, pathParams map[string]string) {
		err := status.Error(codes.Unimplemented, "streaming calls are not yet supported in the in-process transport")
		_, outboundMarshaler := runtime.MarshalerForRequest(mux, req)
		runtime.HTTPError(ctx, mux, outboundMarshaler, w, req, err)
		return
	})

	return nil
}

// RegisterHealthHandlerFromEndpoint is same as RegisterHealthHandler but
// automatically dials to "endpoint" and closes the connection when "ctx" gets done.
func RegisterHealthHandlerFromEndpoint(ctx context.Context, mux *runtime.ServeMux, endpoint string, opts []grpc.DialOption) (err error) {
	conn, err := grpc.Dial(endpoint, opts...)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			if cerr := conn.Close(); cerr != nil {
				grpclog.Infof("Failed to close conn to %s: %v", endpoint, cerr)
			}
			return
		}
		go func() {
			<-ctx.Done()
			if cerr := conn.Close(); cerr != nil {
				grpclog.Infof("Failed to close conn to %s: %v", endpoint, cerr)
			}
		}()
	}()

	return RegisterHealthHandler(ctx, mux, conn)
}

// RegisterHealthHandler registers the http handlers for service Health to "mux".
// The handlers forward requests to the grpc endpoint over "conn".
func RegisterHealthHandler(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error {
	return RegisterHealthHandlerClient(ctx, mux, NewHealthClient(conn))
}

// RegisterHealthHandlerClient registers the http handlers for service Health
// to "mux". The handlers forward requests to the grpc endpoint over the given implementation of "HealthClient".
// Note: the gRPC framework executes interceptors within the gRPC handler. If the passed in "HealthClient"
// doesn't go through the normal gRPC flow (creating a gRPC client etc.) then it will be up to the passed in
// "HealthClient" to call the correct interceptors.
func RegisterHealthHandlerClient(ctx context.Context, mux *runtime.ServeMux, client HealthClient) error {

	mux.Handle("GET", pattern_Health_StreamBeaconLogs_0, func(w http.ResponseWriter, req *http.Request, pathParams map[string]string) {
		ctx, cancel := context.WithCancel(req.Context())
		defer cancel()
		inboundMarshaler, outboundMarshaler := runtime.MarshalerForRequest(mux, req)
		rctx, err := runtime.AnnotateContext(ctx, mux, req, "/ethereum.eth.v1alpha1.Health/StreamBeaconLogs")
		if err != nil {
			runtime.HTTPError(ctx, mux, outboundMarshaler, w, req, err)
			return
		}
		resp, md, err := request_Health_StreamBeaconLogs_0(rctx, inboundMarshaler, client, req, pathParams)
		ctx = runtime.NewServerMetadataContext(ctx, md)
		if err != nil {
			runtime.HTTPError(ctx, mux, outboundMarshaler, w, req, err)
			return
		}

		forward_Health_StreamBeaconLogs_0(ctx, mux, outboundMarshaler, w, req, func() (proto.Message, error) { return resp.Recv() }, mux.GetForwardResponseOptions()...)

	})

	return nil
}

var (
	pattern_Health_StreamBeaconLogs_0 = runtime.MustPattern(runtime.NewPattern(1, []int{2, 0, 2, 1, 2, 2, 2, 3, 2, 4}, []string{"eth", "v1alpha1", "health", "logs", "stream"}, ""))
)

var (
	forward_Health_StreamBeaconLogs_0 = runtime.ForwardResponseStream
)
