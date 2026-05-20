package gateway

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cordum/cordum/core/controlplane/gateway/auth"
	"github.com/cordum/cordum/core/licensing"
)

func TestHandleGetRoleMissingReturnsStableCode(t *testing.T) {
	s, _, _ := newTestGateway(t)

	req := withAuth(httptest.NewRequest(http.MethodGet, "/api/v1/rbac/roles/missing", nil), &auth.AuthContext{
		Tenant: "default",
		Role:   "admin",
	})
	req.SetPathValue("name", "missing")
	rr := httptest.NewRecorder()
	s.handleGetRole(rr, req)

	requireStableErrorCode(t, rr, http.StatusNotFound, "RBAC_ROLE_NOT_FOUND")
}

func TestHandlePutRoleInvalidInheritanceReturnsStableCode(t *testing.T) {
	s, _, _ := newTestGateway(t)
	setTestEntitlements(t, s, licensing.PlanEnterprise, func(entitlements *licensing.Entitlements) {
		entitlements.RBAC = true
	})

	req := withAuth(httptest.NewRequest(http.MethodPut, "/api/v1/rbac/roles/custom", bytes.NewBufferString(`{"inherits":["missing-parent"]}`)), &auth.AuthContext{
		Tenant: "default",
		Role:   "admin",
	})
	req.SetPathValue("name", "custom")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.handlePutRole(rr, req)

	requireStableErrorCode(t, rr, http.StatusBadRequest, "RBAC_PERMISSION_INVALID")
}

func TestHandleDeleteBuiltInRoleReturnsStableCode(t *testing.T) {
	s, _, _ := newTestGateway(t)
	setTestEntitlements(t, s, licensing.PlanEnterprise, func(entitlements *licensing.Entitlements) {
		entitlements.RBAC = true
	})

	req := withAuth(httptest.NewRequest(http.MethodDelete, "/api/v1/rbac/roles/admin", nil), &auth.AuthContext{
		Tenant: "default",
		Role:   "admin",
	})
	req.SetPathValue("name", "admin")
	rr := httptest.NewRecorder()
	s.handleDeleteRole(rr, req)

	requireStableErrorCode(t, rr, http.StatusBadRequest, "RBAC_ROLE_IN_USE")
}
