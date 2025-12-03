// Copyright 2016-2025, Pulumi Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package upsert

import (
	"context"
	"fmt"
	"net"
	"sync"

	pulumix "github.com/pulumi/pulumi-terraform-migrate/pkg/bridgedproviders"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource/plugin"
	pulumirpc "github.com/pulumi/pulumi/sdk/v3/proto/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

// MockProviderServer implements a gRPC provider server that returns predefined
// outputs for Create operations.
type MockProviderServer struct {
	pulumirpc.UnimplementedResourceProviderServer

	providerName   string
	providerBinary string
	resources      map[resource.URN]ResourceSpec
	port           int
	server         *grpc.Server
	listener       net.Listener
	mu             sync.RWMutex
	running        bool
	schema         string // cached schema from the real provider
}

// NewMockProviderServer creates a new mock provider server.
func NewMockProviderServer(providerName string, providerBinary string, resources []ResourceSpec, port int) (*MockProviderServer, error) {
	resourceMap := make(map[resource.URN]ResourceSpec)
	for _, r := range resources {
		resourceMap[r.URN] = r
	}

	return &MockProviderServer{
		providerName:   providerName,
		providerBinary: providerBinary,
		resources:      resourceMap,
		port:           port,
	}, nil
}

// Start starts the mock provider gRPC server.
func (m *MockProviderServer) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return fmt.Errorf("server already running")
	}

	// Create listener
	addr := fmt.Sprintf("127.0.0.1:%d", m.port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	m.listener = listener
	m.port = listener.Addr().(*net.TCPAddr).Port

	// Create gRPC server
	m.server = grpc.NewServer()
	pulumirpc.RegisterResourceProviderServer(m.server, m)

	// Start serving in background
	go func() {
		if err := m.server.Serve(listener); err != nil {
			fmt.Printf("mock provider server error: %v\n", err)
		}
	}()

	m.running = true
	return nil
}

// Stop stops the mock provider server.
func (m *MockProviderServer) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.server != nil {
		m.server.GracefulStop()
		m.server = nil
	}
	if m.listener != nil {
		m.listener.Close()
		m.listener = nil
	}
	m.running = false
}

// Port returns the port the server is listening on.
func (m *MockProviderServer) Port() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.port
}

// loadSchema loads the schema from the real provider binary.
func (m *MockProviderServer) loadSchema(ctx context.Context) error {
	if m.providerBinary == "" {
		return fmt.Errorf("provider binary path not specified")
	}

	// Use the helper function from pulumix package to get the schema
	schema, err := pulumix.GetSchemaFromBinary(ctx, m.providerBinary)
	if err != nil {
		return fmt.Errorf("failed to get schema from provider binary: %w", err)
	}

	// Cache the schema
	m.schema = schema
	return nil
}

// GetSchema implements the GetSchema RPC.
func (m *MockProviderServer) GetSchema(ctx context.Context, req *pulumirpc.GetSchemaRequest) (*pulumirpc.GetSchemaResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Load schema if not already cached
	if m.schema == "" {
		if err := m.loadSchema(ctx); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to load schema: %v", err)
		}
	}

	return &pulumirpc.GetSchemaResponse{
		Schema: m.schema,
	}, nil
}

// CheckConfig implements the CheckConfig RPC.
func (m *MockProviderServer) CheckConfig(ctx context.Context, req *pulumirpc.CheckRequest) (*pulumirpc.CheckResponse, error) {
	// Accept any configuration
	return &pulumirpc.CheckResponse{
		Inputs: req.News,
	}, nil
}

// DiffConfig implements the DiffConfig RPC.
func (m *MockProviderServer) DiffConfig(ctx context.Context, req *pulumirpc.DiffRequest) (*pulumirpc.DiffResponse, error) {
	// No differences in config
	return &pulumirpc.DiffResponse{
		Changes: pulumirpc.DiffResponse_DIFF_NONE,
	}, nil
}

// Configure implements the Configure RPC.
func (m *MockProviderServer) Configure(ctx context.Context, req *pulumirpc.ConfigureRequest) (*pulumirpc.ConfigureResponse, error) {
	// Accept any configuration
	return &pulumirpc.ConfigureResponse{
		AcceptSecrets:   true,
		SupportsPreview: true,
		AcceptResources: true,
	}, nil
}

// Check implements the Check RPC.
func (m *MockProviderServer) Check(ctx context.Context, req *pulumirpc.CheckRequest) (*pulumirpc.CheckResponse, error) {
	// Accept any inputs
	return &pulumirpc.CheckResponse{
		Inputs: req.News,
	}, nil
}

// Diff implements the Diff RPC.
func (m *MockProviderServer) Diff(ctx context.Context, req *pulumirpc.DiffRequest) (*pulumirpc.DiffResponse, error) {
	// Indicate that resource needs to be created
	return &pulumirpc.DiffResponse{
		Changes:  pulumirpc.DiffResponse_DIFF_SOME,
		Replaces: []string{},
	}, nil
}

// Create implements the Create RPC - this is where the magic happens.
func (m *MockProviderServer) Create(ctx context.Context, req *pulumirpc.CreateRequest) (*pulumirpc.CreateResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	urn := resource.URN(req.Urn)

	// Find the resource spec for this URN
	spec, ok := m.resources[urn]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "no mock data for resource %s", urn)
	}

	// Convert PropertyMap to structpb.Struct
	outputs, err := plugin.MarshalProperties(spec.Outputs, plugin.MarshalOptions{
		KeepUnknowns: true,
		KeepSecrets:  true,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal outputs: %v", err)
	}

	return &pulumirpc.CreateResponse{
		Id:         string(spec.ID),
		Properties: outputs,
	}, nil
}

// Read implements the Read RPC.
func (m *MockProviderServer) Read(ctx context.Context, req *pulumirpc.ReadRequest) (*pulumirpc.ReadResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	urn := resource.URN(req.Urn)
	spec, ok := m.resources[urn]
	if !ok {
		// Resource not found - return empty to indicate deletion
		return &pulumirpc.ReadResponse{
			Id:         "",
			Properties: nil,
		}, nil
	}

	// Convert PropertyMap to structpb.Struct
	outputs, err := plugin.MarshalProperties(spec.Outputs, plugin.MarshalOptions{
		KeepUnknowns: true,
		KeepSecrets:  true,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal outputs: %v", err)
	}

	return &pulumirpc.ReadResponse{
		Id:         string(spec.ID),
		Properties: outputs,
	}, nil
}

// Update implements the Update RPC.
func (m *MockProviderServer) Update(ctx context.Context, req *pulumirpc.UpdateRequest) (*pulumirpc.UpdateResponse, error) {
	// For upsert, we shouldn't need to update
	return &pulumirpc.UpdateResponse{
		Properties: req.News,
	}, nil
}

// Delete implements the Delete RPC.
func (m *MockProviderServer) Delete(ctx context.Context, req *pulumirpc.DeleteRequest) (*emptypb.Empty, error) {
	// Mock delete - just return success
	return &emptypb.Empty{}, nil
}

// Invoke implements the Invoke RPC.
func (m *MockProviderServer) Invoke(ctx context.Context, req *pulumirpc.InvokeRequest) (*pulumirpc.InvokeResponse, error) {
	// Return empty for any invocations
	return &pulumirpc.InvokeResponse{
		Return: &structpb.Struct{},
	}, nil
}

// GetPluginInfo implements the GetPluginInfo RPC.
func (m *MockProviderServer) GetPluginInfo(ctx context.Context, req *emptypb.Empty) (*pulumirpc.PluginInfo, error) {
	return &pulumirpc.PluginInfo{
		Version: "1.0.0",
	}, nil
}

// Cancel implements the Cancel RPC.
func (m *MockProviderServer) Cancel(ctx context.Context, req *emptypb.Empty) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

// GetMapping implements the GetMapping RPC.
func (m *MockProviderServer) GetMapping(ctx context.Context, req *pulumirpc.GetMappingRequest) (*pulumirpc.GetMappingResponse, error) {
	return &pulumirpc.GetMappingResponse{
		Provider: m.providerName,
		Data:     []byte("{}"),
	}, nil
}

// Attach implements the Attach RPC.
func (m *MockProviderServer) Attach(ctx context.Context, req *pulumirpc.PluginAttach) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}
