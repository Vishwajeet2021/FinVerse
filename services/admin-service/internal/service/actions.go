// Package service is Admin Service's use-case layer.
//
// Its central rule is audit-then-act (docs/23 D-A2): an administrative action is recorded
// durably, together with an AdminActionRequested event, BEFORE anything is changed
// downstream. If the record cannot be written the action is refused. After the downstream
// call the outcome is recorded and AdminActionResolved is emitted.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

type ActionType string

const (
	ActionAccountFreeze   ActionType = "ACCOUNT_FREEZE"
	ActionAccountUnfreeze ActionType = "ACCOUNT_UNFREEZE"
	ActionFraudResolve    ActionType = "FRAUD_CASE_RESOLVE"
	ActionKYCReview       ActionType = "KYC_REVIEW"
)

type Status string

const (
	StatusRequested Status = "REQUESTED"
	StatusSucceeded Status = "SUCCEEDED"
	StatusFailed    Status = "FAILED"
)

const (
	EventRequested = "AdminActionRequested"
	EventResolved  = "AdminActionResolved"

	MinReasonRunes = 5
	MaxReasonRunes = 500
)

// Action is one privileged operation and its outcome.
type Action struct {
	ID            string          `json:"id"`
	ActorID       string          `json:"actorId"`
	Type          ActionType      `json:"type"`
	TargetType    string          `json:"targetType"`
	TargetID      string          `json:"targetId"`
	Reason        string          `json:"reason"`
	Params        json.RawMessage `json:"params"`
	Status        Status          `json:"status"`
	FailureReason string          `json:"failureReason,omitempty"`
	TraceID       string          `json:"traceId,omitempty"`
	CreatedAt     time.Time       `json:"createdAt"`
	ResolvedAt    *time.Time      `json:"resolvedAt,omitempty"`
}

var (
	// ErrAuditUnavailable: the action could not be recorded, so it was not attempted.
	ErrAuditUnavailable = errors.New("admin action could not be recorded")
	ErrInvalidReason    = fmt.Errorf("reason must be %d-%d characters", MinReasonRunes, MaxReasonRunes)
	ErrInvalidTarget    = errors.New("invalid target id")
	ErrInvalidInput     = errors.New("invalid input")
)

// DownstreamError is a non-success answer from the service that performs the action.
type DownstreamError struct {
	Status  int
	Code    string
	Message string
}

func (e *DownstreamError) Error() string {
	return fmt.Sprintf("downstream returned %d %s", e.Status, e.Code)
}

// Server reports a downstream failure that says nothing about the request itself.
func (e *DownstreamError) Server() bool { return e.Status >= 500 }

type Repository interface {
	// Begin stores the action (status REQUESTED) and the AdminActionRequested event
	// atomically.
	Begin(ctx context.Context, a Action) error
	// Finish moves a REQUESTED action to its final status and emits AdminActionResolved,
	// atomically. It never touches an action that is already final.
	Finish(ctx context.Context, id string, status Status, failureReason string) error
	List(ctx context.Context, limit int, before *time.Time) ([]Action, error)
}

type Accounts interface {
	Freeze(ctx context.Context, accountID, reason string) error
	Unfreeze(ctx context.Context, accountID string) error
}

// Forwarded calls carry the administrator's own bearer token, so the downstream service
// enforces its own scope and records who acted (docs/23 D-A3).
type Fraud interface {
	OpenCases(ctx context.Context, bearer string, limit int) (json.RawMessage, int, error)
	Resolve(ctx context.Context, bearer, caseID, outcome string) error
}

type Customers interface {
	ReviewKYC(ctx context.Context, bearer, customerID string, approve bool, tier int) error
}

type Service struct {
	repo      Repository
	accounts  Accounts
	fraud     Fraud
	customers Customers
	now       func() time.Time
	logf      func(msg string, kv ...any)
}

func New(repo Repository, accounts Accounts, fraud Fraud, customers Customers, logf func(string, ...any)) *Service {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Service{repo: repo, accounts: accounts, fraud: fraud, customers: customers, now: time.Now, logf: logf}
}

func validReason(reason string) (string, error) {
	r := strings.TrimSpace(reason)
	if n := utf8.RuneCountInString(r); n < MinReasonRunes || n > MaxReasonRunes {
		return "", ErrInvalidReason
	}
	for _, c := range r {
		if c < 0x20 && c != '\n' { // control characters have no place in an audit reason
			return "", ErrInvalidReason
		}
	}
	return r, nil
}

func validID(id string) bool {
	_, err := uuid.Parse(id)
	return err == nil
}

// execute is the audit-then-act sequence shared by every command.
func (s *Service) execute(ctx context.Context, actorID string, typ ActionType, targetType, targetID, reason string, params any, trace string, call func(context.Context) error) (*Action, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("encoding params: %w", err)
	}
	a := Action{
		ID: uuid.NewString(), ActorID: actorID, Type: typ, TargetType: targetType, TargetID: targetID,
		Reason: reason, Params: raw, Status: StatusRequested, TraceID: trace, CreatedAt: s.now().UTC(),
	}
	if err := s.repo.Begin(ctx, a); err != nil {
		s.logf("admin action not recorded; refusing to act", "action_type", typ, "error", err)
		return nil, ErrAuditUnavailable
	}

	callErr := call(ctx)

	status, failure := StatusSucceeded, ""
	if callErr != nil {
		status, failure = StatusFailed, truncate(failureText(callErr), 200)
	}
	// The outcome is recorded with a context that survives the client disconnecting: the action
	// already happened (or failed) and the record must say so.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if finErr := s.repo.Finish(finishCtx, a.ID, status, failure); finErr != nil {
		// The row stays REQUESTED, which is itself visible evidence of an unresolved action.
		s.logf("recording admin action outcome failed", "action_id", a.ID, "error", finErr)
	} else {
		now := s.now().UTC()
		a.ResolvedAt = &now
		a.Status, a.FailureReason = status, failure
	}
	return &a, callErr
}

func failureText(err error) string {
	var d *DownstreamError
	if errors.As(err, &d) {
		return fmt.Sprintf("%d %s", d.Status, d.Code)
	}
	return "downstream unreachable"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func (s *Service) FreezeAccount(ctx context.Context, actorID, accountID, reason, trace string) (*Action, error) {
	r, err := validReason(reason)
	if err != nil {
		return nil, err
	}
	if !validID(accountID) {
		return nil, ErrInvalidTarget
	}
	return s.execute(ctx, actorID, ActionAccountFreeze, "ACCOUNT", accountID, r, map[string]any{}, trace,
		func(ctx context.Context) error { return s.accounts.Freeze(ctx, accountID, r) })
}

func (s *Service) UnfreezeAccount(ctx context.Context, actorID, accountID, reason, trace string) (*Action, error) {
	r, err := validReason(reason)
	if err != nil {
		return nil, err
	}
	if !validID(accountID) {
		return nil, ErrInvalidTarget
	}
	return s.execute(ctx, actorID, ActionAccountUnfreeze, "ACCOUNT", accountID, r, map[string]any{}, trace,
		func(ctx context.Context) error { return s.accounts.Unfreeze(ctx, accountID) })
}

func (s *Service) ResolveFraudCase(ctx context.Context, actorID, bearer, caseID, outcome, reason, trace string) (*Action, error) {
	r, err := validReason(reason)
	if err != nil {
		return nil, err
	}
	if !validID(caseID) {
		return nil, ErrInvalidTarget
	}
	if outcome != "CONFIRMED" && outcome != "DISMISSED" {
		return nil, fmt.Errorf("%w: outcome must be CONFIRMED or DISMISSED", ErrInvalidInput)
	}
	return s.execute(ctx, actorID, ActionFraudResolve, "FRAUD_CASE", caseID, r, map[string]any{"outcome": outcome}, trace,
		func(ctx context.Context) error { return s.fraud.Resolve(ctx, bearer, caseID, outcome) })
}

func (s *Service) ReviewKYC(ctx context.Context, actorID, bearer, customerID string, approve bool, tier int, reason, trace string) (*Action, error) {
	r, err := validReason(reason)
	if err != nil {
		return nil, err
	}
	if !validID(customerID) {
		return nil, ErrInvalidTarget
	}
	if approve && (tier < 1 || tier > 3) {
		return nil, fmt.Errorf("%w: an approval needs a tier from 1 to 3", ErrInvalidInput)
	}
	if !approve {
		tier = 0
	}
	return s.execute(ctx, actorID, ActionKYCReview, "CUSTOMER", customerID, r, map[string]any{"approve": approve, "tier": tier}, trace,
		func(ctx context.Context) error { return s.customers.ReviewKYC(ctx, bearer, customerID, approve, tier) })
}

func (s *Service) ListActions(ctx context.Context, limit int, before *time.Time) ([]Action, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	return s.repo.List(ctx, limit, before)
}
