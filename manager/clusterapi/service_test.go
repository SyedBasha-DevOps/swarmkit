package clusterapi

import (
	"testing"

	"github.com/docker/swarm-v2/api"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func createSpec(name, image string, instances int64) *api.ServiceSpec {
	return &api.ServiceSpec{
		Annotations: api.Annotations{
			Name: name,
		},
		Template: &api.TaskSpec{
			Runtime: &api.TaskSpec_Container{
				Container: &api.Container{
					Image: &api.Image{
						Reference: image,
					},
				},
			},
		},
		Instances: instances,
	}
}

func createService(t *testing.T, ts *testServer, name, image string, instances int64) *api.Service {
	spec := createSpec(name, image, instances)
	r, err := ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec})
	assert.NoError(t, err)
	return r.Service
}

func TestValidateResources(t *testing.T) {
	bad := []*api.Resources{
		{MemoryBytes: 1},
		{NanoCPUs: 42},
	}

	good := []*api.Resources{
		{MemoryBytes: 4096 * 1024 * 1024},
		{NanoCPUs: 1e9},
	}

	for _, b := range bad {
		err := validateResources(b)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, grpc.Code(err))
	}

	for _, g := range good {
		assert.NoError(t, validateResources(g))
	}
}

func TestValidateResourceRequirements(t *testing.T) {
	bad := []*api.ResourceRequirements{
		{Limits: &api.Resources{MemoryBytes: 1}},
		{Reservations: &api.Resources{MemoryBytes: 1}},
	}
	good := []*api.ResourceRequirements{
		{Limits: &api.Resources{NanoCPUs: 1e9}},
		{Reservations: &api.Resources{NanoCPUs: 1e9}},
	}
	for _, b := range bad {
		err := validateResourceRequirements(b)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, grpc.Code(err))
	}

	for _, g := range good {
		assert.NoError(t, validateResourceRequirements(g))
	}
}

func TestValidateServiceSpecTemplate(t *testing.T) {
	type badSource struct {
		s *api.ServiceSpec
		c codes.Code
	}

	for _, bad := range []badSource{
		{
			s: &api.ServiceSpec{Template: nil},
			c: codes.InvalidArgument,
		},
		{
			s: &api.ServiceSpec{
				Template: &api.TaskSpec{
					Runtime: nil,
				},
			},
			c: codes.InvalidArgument,
		},
		// NOTE(stevvooe): can't actually test this case because we don't have
		// another runtime defined.
		// {
		//	s: &api.ServiceSpec{
		//		Template: &api.TaskSpec{
		//			Runtime:
		//		},
		//	},
		//	c: codes.Unimplemented,
		// },
		{
			s: createSpec("", "", 0),
			c: codes.InvalidArgument,
		},
	} {
		err := validateServiceSpecTemplate(bad.s)
		assert.Error(t, err)
		assert.Equal(t, bad.c, grpc.Code(err))
	}

	for _, good := range []*api.ServiceSpec{
		createSpec("", "image", 0),
	} {
		err := validateServiceSpecTemplate(good)
		assert.NoError(t, err)
	}
}

func TestValidateServiceSpec(t *testing.T) {
	type BadServiceSpec struct {
		spec *api.ServiceSpec
		c    codes.Code
	}

	for _, bad := range []BadServiceSpec{
		{
			spec: nil,
			c:    codes.InvalidArgument,
		},
		{
			spec: &api.ServiceSpec{Annotations: api.Annotations{Name: "name"}},
			c:    codes.InvalidArgument,
		},
		{
			spec: createSpec("", "", 1),
			c:    codes.InvalidArgument,
		},
		{
			spec: createSpec("name", "", 1),
			c:    codes.InvalidArgument,
		},
		{
			spec: createSpec("", "image", 1),
			c:    codes.InvalidArgument,
		},
	} {
		err := validateServiceSpec(bad.spec)
		assert.Error(t, err)
		assert.Equal(t, bad.c, grpc.Code(err))
	}

	for _, good := range []*api.ServiceSpec{
		createSpec("name", "image", 1),
	} {
		err := validateServiceSpec(good)
		assert.NoError(t, err)
	}
}

func TestCreateService(t *testing.T) {
	ts := newTestServer(t)
	_, err := ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	spec := createSpec("name", "image", 1)
	r, err := ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec})
	assert.NoError(t, err)
	assert.NotEmpty(t, r.Service.ID)
}

func TestGetService(t *testing.T) {
	ts := newTestServer(t)
	_, err := ts.Client.GetService(context.Background(), &api.GetServiceRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	_, err = ts.Client.GetService(context.Background(), &api.GetServiceRequest{ServiceID: "invalid"})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, grpc.Code(err))

	service := createService(t, ts, "name", "image", 1)
	r, err := ts.Client.GetService(context.Background(), &api.GetServiceRequest{ServiceID: service.ID})
	assert.NoError(t, err)
	service.Version = r.Service.Version
	assert.Equal(t, service, r.Service)
}

func TestUpdateService(t *testing.T) {
	ts := newTestServer(t)
	service := createService(t, ts, "name", "image", 1)

	_, err := ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{ServiceID: "invalid", Spec: &service.Spec, ServiceVersion: &api.Version{}})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, grpc.Code(err))

	// No update options.
	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{ServiceID: service.ID, Spec: &service.Spec})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{ServiceID: service.ID, Spec: &service.Spec, ServiceVersion: &service.Version})
	assert.NoError(t, err)

	r, err := ts.Client.GetService(context.Background(), &api.GetServiceRequest{ServiceID: service.ID})
	assert.NoError(t, err)
	assert.Equal(t, service.Spec.Annotations.Name, r.Service.Spec.Annotations.Name)
	assert.EqualValues(t, 1, r.Service.Spec.Instances)

	r.Service.Spec.Instances = 42
	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{
		ServiceID:      service.ID,
		Spec:           &r.Service.Spec,
		ServiceVersion: &r.Service.Version,
	})
	assert.NoError(t, err)

	r, err = ts.Client.GetService(context.Background(), &api.GetServiceRequest{ServiceID: service.ID})
	assert.NoError(t, err)
	assert.Equal(t, service.Spec.Annotations.Name, r.Service.Spec.Annotations.Name)
	assert.EqualValues(t, 42, r.Service.Spec.Instances)

	// Versioning.
	r, err = ts.Client.GetService(context.Background(), &api.GetServiceRequest{ServiceID: service.ID})
	assert.NoError(t, err)
	version := &r.Service.Version

	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{
		ServiceID:      service.ID,
		Spec:           &r.Service.Spec,
		ServiceVersion: version,
	})
	assert.NoError(t, err)

	// Perform an update with the "old" version.
	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{
		ServiceID:      service.ID,
		Spec:           &r.Service.Spec,
		ServiceVersion: version,
	})
	assert.Error(t, err)
}

func TestRemoveService(t *testing.T) {
	ts := newTestServer(t)
	_, err := ts.Client.RemoveService(context.Background(), &api.RemoveServiceRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	service := createService(t, ts, "name", "image", 1)
	r, err := ts.Client.RemoveService(context.Background(), &api.RemoveServiceRequest{ServiceID: service.ID})
	assert.NoError(t, err)
	assert.NotNil(t, r)
}

func TestListServices(t *testing.T) {
	ts := newTestServer(t)
	r, err := ts.Client.ListServices(context.Background(), &api.ListServicesRequest{})
	assert.NoError(t, err)
	assert.Empty(t, r.Services)

	createService(t, ts, "name1", "image", 1)
	r, err = ts.Client.ListServices(context.Background(), &api.ListServicesRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(r.Services))

	createService(t, ts, "name2", "image", 1)
	createService(t, ts, "name3", "image", 1)
	r, err = ts.Client.ListServices(context.Background(), &api.ListServicesRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 3, len(r.Services))
}