// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.5.1
// - protoc             v3.21.12
// source: tee/proto/region_service.proto

package proto

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.64.0 or later.
const _ = grpc.SupportPackageIsVersion9

const (
	RegionService_CreateRegion_FullMethodName    = "/teeregion.RegionService/CreateRegion"
	RegionService_UpdateRegion_FullMethodName    = "/teeregion.RegionService/UpdateRegion"
	RegionService_DeleteRegion_FullMethodName    = "/teeregion.RegionService/DeleteRegion"
	RegionService_GetRegionStatus_FullMethodName = "/teeregion.RegionService/GetRegionStatus"
	RegionService_ListRegions_FullMethodName     = "/teeregion.RegionService/ListRegions"
)

// RegionServiceClient is the client API for RegionService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type RegionServiceClient interface {
	// Create/update/delete regions
	CreateRegion(ctx context.Context, in *CreateRegionRequest, opts ...grpc.CallOption) (*CreateRegionResponse, error)
	UpdateRegion(ctx context.Context, in *UpdateRegionRequest, opts ...grpc.CallOption) (*UpdateRegionResponse, error)
	DeleteRegion(ctx context.Context, in *DeleteRegionRequest, opts ...grpc.CallOption) (*DeleteRegionResponse, error)
	// Region status/health
	GetRegionStatus(ctx context.Context, in *GetRegionStatusRequest, opts ...grpc.CallOption) (*GetRegionStatusResponse, error)
	ListRegions(ctx context.Context, in *ListRegionsRequest, opts ...grpc.CallOption) (*ListRegionsResponse, error)
}

type regionServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewRegionServiceClient(cc grpc.ClientConnInterface) RegionServiceClient {
	return &regionServiceClient{cc}
}

func (c *regionServiceClient) CreateRegion(ctx context.Context, in *CreateRegionRequest, opts ...grpc.CallOption) (*CreateRegionResponse, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(CreateRegionResponse)
	err := c.cc.Invoke(ctx, RegionService_CreateRegion_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *regionServiceClient) UpdateRegion(ctx context.Context, in *UpdateRegionRequest, opts ...grpc.CallOption) (*UpdateRegionResponse, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(UpdateRegionResponse)
	err := c.cc.Invoke(ctx, RegionService_UpdateRegion_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *regionServiceClient) DeleteRegion(ctx context.Context, in *DeleteRegionRequest, opts ...grpc.CallOption) (*DeleteRegionResponse, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(DeleteRegionResponse)
	err := c.cc.Invoke(ctx, RegionService_DeleteRegion_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *regionServiceClient) GetRegionStatus(ctx context.Context, in *GetRegionStatusRequest, opts ...grpc.CallOption) (*GetRegionStatusResponse, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(GetRegionStatusResponse)
	err := c.cc.Invoke(ctx, RegionService_GetRegionStatus_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *regionServiceClient) ListRegions(ctx context.Context, in *ListRegionsRequest, opts ...grpc.CallOption) (*ListRegionsResponse, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(ListRegionsResponse)
	err := c.cc.Invoke(ctx, RegionService_ListRegions_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RegionServiceServer is the server API for RegionService service.
// All implementations must embed UnimplementedRegionServiceServer
// for forward compatibility.
type RegionServiceServer interface {
	// Create/update/delete regions
	CreateRegion(context.Context, *CreateRegionRequest) (*CreateRegionResponse, error)
	UpdateRegion(context.Context, *UpdateRegionRequest) (*UpdateRegionResponse, error)
	DeleteRegion(context.Context, *DeleteRegionRequest) (*DeleteRegionResponse, error)
	// Region status/health
	GetRegionStatus(context.Context, *GetRegionStatusRequest) (*GetRegionStatusResponse, error)
	ListRegions(context.Context, *ListRegionsRequest) (*ListRegionsResponse, error)
	mustEmbedUnimplementedRegionServiceServer()
}

// UnimplementedRegionServiceServer must be embedded to have
// forward compatible implementations.
//
// NOTE: this should be embedded by value instead of pointer to avoid a nil
// pointer dereference when methods are called.
type UnimplementedRegionServiceServer struct{}

func (UnimplementedRegionServiceServer) CreateRegion(context.Context, *CreateRegionRequest) (*CreateRegionResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CreateRegion not implemented")
}
func (UnimplementedRegionServiceServer) UpdateRegion(context.Context, *UpdateRegionRequest) (*UpdateRegionResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UpdateRegion not implemented")
}
func (UnimplementedRegionServiceServer) DeleteRegion(context.Context, *DeleteRegionRequest) (*DeleteRegionResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeleteRegion not implemented")
}
func (UnimplementedRegionServiceServer) GetRegionStatus(context.Context, *GetRegionStatusRequest) (*GetRegionStatusResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetRegionStatus not implemented")
}
func (UnimplementedRegionServiceServer) ListRegions(context.Context, *ListRegionsRequest) (*ListRegionsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListRegions not implemented")
}
func (UnimplementedRegionServiceServer) mustEmbedUnimplementedRegionServiceServer() {}
func (UnimplementedRegionServiceServer) testEmbeddedByValue()                       {}

// UnsafeRegionServiceServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to RegionServiceServer will
// result in compilation errors.
type UnsafeRegionServiceServer interface {
	mustEmbedUnimplementedRegionServiceServer()
}

func RegisterRegionServiceServer(s grpc.ServiceRegistrar, srv RegionServiceServer) {
	// If the following call pancis, it indicates UnimplementedRegionServiceServer was
	// embedded by pointer and is nil.  This will cause panics if an
	// unimplemented method is ever invoked, so we test this at initialization
	// time to prevent it from happening at runtime later due to I/O.
	if t, ok := srv.(interface{ testEmbeddedByValue() }); ok {
		t.testEmbeddedByValue()
	}
	s.RegisterService(&RegionService_ServiceDesc, srv)
}

func _RegionService_CreateRegion_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CreateRegionRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(RegionServiceServer).CreateRegion(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: RegionService_CreateRegion_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(RegionServiceServer).CreateRegion(ctx, req.(*CreateRegionRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _RegionService_UpdateRegion_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(UpdateRegionRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(RegionServiceServer).UpdateRegion(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: RegionService_UpdateRegion_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(RegionServiceServer).UpdateRegion(ctx, req.(*UpdateRegionRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _RegionService_DeleteRegion_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DeleteRegionRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(RegionServiceServer).DeleteRegion(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: RegionService_DeleteRegion_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(RegionServiceServer).DeleteRegion(ctx, req.(*DeleteRegionRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _RegionService_GetRegionStatus_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetRegionStatusRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(RegionServiceServer).GetRegionStatus(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: RegionService_GetRegionStatus_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(RegionServiceServer).GetRegionStatus(ctx, req.(*GetRegionStatusRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _RegionService_ListRegions_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListRegionsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(RegionServiceServer).ListRegions(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: RegionService_ListRegions_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(RegionServiceServer).ListRegions(ctx, req.(*ListRegionsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// RegionService_ServiceDesc is the grpc.ServiceDesc for RegionService service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var RegionService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "teeregion.RegionService",
	HandlerType: (*RegionServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "CreateRegion",
			Handler:    _RegionService_CreateRegion_Handler,
		},
		{
			MethodName: "UpdateRegion",
			Handler:    _RegionService_UpdateRegion_Handler,
		},
		{
			MethodName: "DeleteRegion",
			Handler:    _RegionService_DeleteRegion_Handler,
		},
		{
			MethodName: "GetRegionStatus",
			Handler:    _RegionService_GetRegionStatus_Handler,
		},
		{
			MethodName: "ListRegions",
			Handler:    _RegionService_ListRegions_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "tee/proto/region_service.proto",
}
