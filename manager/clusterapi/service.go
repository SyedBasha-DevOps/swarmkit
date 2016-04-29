package clusterapi

import (
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/identity"
	"github.com/docker/swarm-v2/manager/state"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func validateResources(r *api.Resources) error {
	if r == nil {
		return nil
	}

	if r.NanoCPUs != 0 && r.NanoCPUs < 1e6 {
		return grpc.Errorf(codes.InvalidArgument, "invalid cpu value %d: Must be at least %d", r.NanoCPUs, 1e6)
	}

	if r.MemoryBytes != 0 && r.MemoryBytes < 4*1024*1024 {
		return grpc.Errorf(codes.InvalidArgument, "invalid memory value %d: Must be at least 4MiB", r.MemoryBytes)
	}
	return nil
}

func validateResourceRequirements(r *api.ResourceRequirements) error {
	if r == nil {
		return nil
	}
	if err := validateResources(r.Limits); err != nil {
		return err
	}
	if err := validateResources(r.Reservations); err != nil {
		return err
	}
	return nil
}

func validateServiceSpecTemplate(spec *api.ServiceSpec) error {
	tpl := spec.Template

	if tpl == nil {
		return grpc.Errorf(codes.InvalidArgument, "missing template in service spec")
	}

	if tpl.GetRuntime() == nil {
		return grpc.Errorf(codes.InvalidArgument, "template: runtime container spec required in service spec task template")
	}

	container := tpl.GetContainer()
	if container == nil {
		return grpc.Errorf(codes.Unimplemented, "template: unimplemented runtime in service spec task template")
	}

	if err := validateResourceRequirements(container.Resources); err != nil {
		return err
	}

	image := container.Image
	if image == nil {
		return grpc.Errorf(codes.Unimplemented, "template: container image not specified")
	}
	if image.Reference == "" {
		return grpc.Errorf(codes.InvalidArgument, "template: image reference must be provided")
	}
	return nil
}

func validateServiceSpec(spec *api.ServiceSpec) error {
	if spec == nil {
		return grpc.Errorf(codes.InvalidArgument, errInvalidArgument.Error())
	}
	if err := validateAnnotations(spec.Annotations); err != nil {
		return err
	}
	if err := validateServiceSpecTemplate(spec); err != nil {
		return err
	}
	return nil
}

// CreateService creates and return a Service based on the provided ServiceSpec.
// - Returns `InvalidArgument` if the ServiceSpec is malformed.
// - Returns `Unimplemented` if the ServiceSpec references unimplemented features.
// - Returns `AlreadyExists` if the ServiceID conflicts.
// - Returns an error if the creation fails.
func (s *Server) CreateService(ctx context.Context, request *api.CreateServiceRequest) (*api.CreateServiceResponse, error) {
	if err := validateServiceSpec(request.Spec); err != nil {
		return nil, err
	}

	// TODO(aluzzardi): Consider using `Name` as a primary key to handle
	// duplicate creations. See #65
	service := &api.Service{
		ID:   identity.NewID(),
		Spec: *request.Spec,
	}

	err := s.store.Update(func(tx state.Tx) error {
		return tx.Services().Create(service)
	})
	if err != nil {
		return nil, err
	}

	return &api.CreateServiceResponse{
		Service: service,
	}, nil
}

// GetService returns a Service given a ServiceID.
// - Returns `InvalidArgument` if ServiceID is not provided.
// - Returns `NotFound` if the Service is not found.
func (s *Server) GetService(ctx context.Context, request *api.GetServiceRequest) (*api.GetServiceResponse, error) {
	if request.ServiceID == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, errInvalidArgument.Error())
	}

	var service *api.Service
	err := s.store.View(func(tx state.ReadTx) error {
		service = tx.Services().Get(request.ServiceID)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if service == nil {
		return nil, grpc.Errorf(codes.NotFound, "service %s not found", request.ServiceID)
	}

	return &api.GetServiceResponse{
		Service: service,
	}, nil
}

// UpdateService updates a Service referenced by ServiceID with the given ServiceSpec.
// - Returns `NotFound` if the Service is not found.
// - Returns `InvalidArgument` if the ServiceSpec is malformed.
// - Returns `Unimplemented` if the ServiceSpec references unimplemented features.
// - Returns an error if the update fails.
func (s *Server) UpdateService(ctx context.Context, request *api.UpdateServiceRequest) (*api.UpdateServiceResponse, error) {
	if request.ServiceID == "" || request.ServiceVersion == nil {
		return nil, grpc.Errorf(codes.InvalidArgument, errInvalidArgument.Error())
	}
	if err := validateServiceSpec(request.Spec); err != nil {
		return nil, err
	}

	var service *api.Service
	err := s.store.Update(func(tx state.Tx) error {
		services := tx.Services()
		service = services.Get(request.ServiceID)
		if service == nil {
			return nil
		}
		service.Version = *request.ServiceVersion
		service.Spec = *request.Spec.Copy()
		return services.Update(service)
	})
	if err != nil {
		return nil, err
	}
	if service == nil {
		return nil, grpc.Errorf(codes.NotFound, "service %s not found", request.ServiceID)
	}
	return &api.UpdateServiceResponse{
		Service: service,
	}, nil
}

// RemoveService removes a Service referenced by ServiceID.
// - Returns `InvalidArgument` if ServiceID is not provided.
// - Returns `NotFound` if the Service is not found.
// - Returns an error if the deletion fails.
func (s *Server) RemoveService(ctx context.Context, request *api.RemoveServiceRequest) (*api.RemoveServiceResponse, error) {
	if request.ServiceID == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, errInvalidArgument.Error())
	}

	err := s.store.Update(func(tx state.Tx) error {
		return tx.Services().Delete(request.ServiceID)
	})
	if err != nil {
		if err == state.ErrNotExist {
			return nil, grpc.Errorf(codes.NotFound, "service %s not found", request.ServiceID)
		}
		return nil, err
	}
	return &api.RemoveServiceResponse{}, nil
}

// ListServices returns a list of all services.
func (s *Server) ListServices(ctx context.Context, request *api.ListServicesRequest) (*api.ListServicesResponse, error) {
	var services []*api.Service
	err := s.store.View(func(tx state.ReadTx) error {
		var err error
		if request.Options == nil || request.Options.Query == "" {
			services, err = tx.Services().Find(state.All)
		} else {
			services, err = tx.Services().Find(state.ByQuery(request.Options.Query))
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return &api.ListServicesResponse{
		Services: services,
	}, nil
}