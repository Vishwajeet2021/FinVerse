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
		// accounts:*/wallets:*/payments:* added during Phase 2 implementation
		// as Account/Wallet/Payment Service shipped — see docs/10-authentication-authorization.md §3.2
		// for the catalog entry each scope corresponds to.
		return []string{
			"customers:read", "customers:write",
			"accounts:read", "accounts:manage",
			"wallets:read", "wallets:manage",
			"payments:read", "payments:write",
			"notifications:read",
			"reports:read", "reports:write",
		}
	case domain.RoleSupport:
		return []string{"customers:read", "accounts:read"}
	case domain.RoleFraudAnalyst:
		return []string{"customers:read", "fraud:read", "fraud:resolve"}
	case domain.RoleAuditor:
		return []string{"customers:read", "audit:read"}
	case domain.RoleAdmin, domain.RoleSuperAdmin:
		return []string{
			"customers:read", "customers:write", "customers:kyc:review",
			"accounts:read", "accounts:manage",
			"notifications:read", "notifications:admin",
			"audit:read", "fraud:read", "fraud:resolve",
			// docs/23: console, admin-initiated freezes, own statements and operational reports.
			"admin:dashboard:read", "admin:accounts:freeze",
			"reports:read", "reports:write", "reports:admin",
		}
	default:
		return nil
	}
}

// RefreshTokenTTLForRole implements the shorter admin session lifetime in
// docs/10-authentication-authorization.md §6.
func RefreshTokenTTLForRole(role domain.Role) bool {
	return role == domain.RoleAdmin || role == domain.RoleSuperAdmin
}
