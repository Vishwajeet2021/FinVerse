package service

import (
	"strings"
	"testing"

	"github.com/finverse/auth-service/internal/domain"
)

func has(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want {
			return true
		}
	}
	return false
}

// Privileged scopes must never reach customers or read-only staff (docs/23 §4).
func TestPrivilegedScopesAreAdminOnly(t *testing.T) {
	privileged := []string{"admin:dashboard:read", "admin:accounts:freeze", "reports:admin", "customers:kyc:review", "fraud:resolve"}
	for _, role := range []domain.Role{domain.RoleCustomer, domain.RoleSupport, domain.RoleAuditor} {
		for _, p := range privileged {
			if has(scopesForRole(role), p) {
				t.Errorf("role %s must not hold %s", role, p)
			}
		}
	}
	for _, role := range []domain.Role{domain.RoleAdmin, domain.RoleSuperAdmin} {
		for _, p := range []string{"admin:dashboard:read", "admin:accounts:freeze", "reports:admin"} {
			if !has(scopesForRole(role), p) {
				t.Errorf("role %s should hold %s", role, p)
			}
		}
	}
}

func TestCustomersCanRequestStatementsButNoAdminScopes(t *testing.T) {
	s := scopesForRole(domain.RoleCustomer)
	if !has(s, "reports:read") || !has(s, "reports:write") {
		t.Error("customers need reports:read and reports:write for their own statements")
	}
	for _, sc := range s {
		if strings.HasPrefix(sc, "admin:") {
			t.Errorf("customer holds %s", sc)
		}
	}
}
