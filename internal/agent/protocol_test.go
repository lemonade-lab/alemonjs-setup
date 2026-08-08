package agent

import (
	"context"
	"testing"
)

func TestRoleProtocolReadOnly(t *testing.T) {
	if !RoleReadOnly(RoleReviewer) || RoleReadOnly(RoleImplementer) {
		t.Fatal("unexpected role permissions")
	}
}

func TestCapabilityRegistry(t *testing.T) {
	r := NewCapabilityRegistry()
	c := stubCapability{}
	r.Register(c)
	if _, ok := r.Get(c.Name()); !ok {
		t.Fatal("capability not registered")
	}
}

type stubCapability struct{}

func (stubCapability) Name() string   { return "stub" }
func (stubCapability) ReadOnly() bool { return true }
func (stubCapability) Invoke(_ context.Context, _ string, _ map[string]any) (any, error) {
	return nil, nil
}
