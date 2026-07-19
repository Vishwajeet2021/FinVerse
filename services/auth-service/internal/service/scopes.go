package service

import "github.com/finverse/auth-service/internal/domain"

// scopesForRole implements the scope catalog in
// docs/10-authentication-authorization.md §3.2. It's intentionally small
// right now — only Auth and Customer Service exist (docs/19-implementation-roadmap.md
// Phase 1) — and grows one line at a time as each subsequent service ships,
// rather than pre-declaring scopes for services that don't exist yet.
func scopesForRole(role domain.Role) []string {
	switch role {
	case domain.RoleCustomer:
		return []string{"customers:read", "customers:write"}
	case domain.RoleSupport:
		return []string{"customers:read"}
	case domain.RoleFraudAnalyst:
		return []string{"customers:read"}
	case domain.RoleAuditor:
		return []string{"customers:read", "audit:read"}
	case domain.RoleAdmin, domain.RoleSuperAdmin:
		return []string{"customers:read", "customers:write", "customers:kyc:review"}
	default:
		return nil
	}
}

// RefreshTokenTTLForRole implements the shorter admin session lifetime in
// docs/10-authentication-authorization.md §6.
func RefreshTokenTTLForRole(role domain.Role) bool {
	return role == domain.RoleAdmin || role == domain.RoleSuperAdmin
}
