package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeRepo struct {
	actions               map[string]*Action
	events                []string // event types in emission order
	failBegin, failFinish bool
	order                 *[]string
}

func newFakeRepo(order *[]string) *fakeRepo {
	return &fakeRepo{actions: map[string]*Action{}, order: order}
}

func (f *fakeRepo) Begin(_ context.Context, a Action) error {
	if f.failBegin {
		return errors.New("db down")
	}
	cp := a
	f.actions[a.ID] = &cp
	f.events = append(f.events, EventRequested)
	*f.order = append(*f.order, "begin")
	return nil
}

func (f *fakeRepo) Finish(_ context.Context, id string, st Status, fr string) error {
	if f.failFinish {
		return errors.New("db down")
	}
	a := f.actions[id]
	if a.Status != StatusRequested {
		return errors.New("already final")
	}
	a.Status, a.FailureReason = st, fr
	f.events = append(f.events, EventResolved)
	*f.order = append(*f.order, "finish")
	return nil
}

func (f *fakeRepo) List(context.Context, int, *time.Time) ([]Action, error) { return nil, nil }

type fakeAccounts struct {
	err   error
	order *[]string
	calls int
}

func (f *fakeAccounts) Freeze(context.Context, string, string) error {
	f.calls++
	*f.order = append(*f.order, "call")
	return f.err
}
func (f *fakeAccounts) Unfreeze(context.Context, string) error { return f.Freeze(nil, "", "") }

type fakeFraud struct{ gotBearer string }

func (f *fakeFraud) OpenCases(context.Context, string, int) (json.RawMessage, int, error) {
	return json.RawMessage(`[]`), 0, nil
}
func (f *fakeFraud) Resolve(_ context.Context, bearer, _, _ string) error {
	f.gotBearer = bearer
	return nil
}

type fakeCustomers struct {
	approve bool
	tier    int
}

func (f *fakeCustomers) ReviewKYC(_ context.Context, _, _ string, approve bool, tier int) error {
	f.approve, f.tier = approve, tier
	return nil
}

func setup() (*Service, *fakeRepo, *fakeAccounts, *fakeFraud, *fakeCustomers, *[]string) {
	order := &[]string{}
	repo, acc, fr, cu := newFakeRepo(order), &fakeAccounts{order: order}, &fakeFraud{}, &fakeCustomers{}
	return New(repo, acc, fr, cu, nil), repo, acc, fr, cu, order
}

var (
	admin  = uuid.NewString()
	target = uuid.NewString()
)

func TestAuditHappensBeforeTheAction(t *testing.T) {
	svc, repo, acc, _, _, order := setup()
	a, err := svc.FreezeAccount(context.Background(), admin, target, "fraud ring suspected", "trace1")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(*order, ","); got != "begin,call,finish" {
		t.Fatalf("sequence = %s, want begin,call,finish (audit-then-act)", got)
	}
	if a.Status != StatusSucceeded || repo.actions[a.ID].Status != StatusSucceeded {
		t.Errorf("status = %s", a.Status)
	}
	if strings.Join(repo.events, ",") != EventRequested+","+EventResolved {
		t.Errorf("events = %v", repo.events)
	}
	if acc.calls != 1 {
		t.Errorf("downstream calls = %d", acc.calls)
	}
}

func TestFailsClosedWhenTheAuditRecordCannotBeWritten(t *testing.T) {
	svc, repo, acc, _, _, _ := setup()
	repo.failBegin = true
	_, err := svc.FreezeAccount(context.Background(), admin, target, "fraud ring suspected", "")
	if !errors.Is(err, ErrAuditUnavailable) {
		t.Fatalf("err = %v, want ErrAuditUnavailable", err)
	}
	if acc.calls != 0 {
		t.Fatal("the account was frozen even though the action could not be audited")
	}
}

func TestDownstreamFailureIsRecordedAndReturned(t *testing.T) {
	svc, repo, acc, _, _, _ := setup()
	acc.err = &DownstreamError{Status: 409, Code: "ACCOUNT_ALREADY_FROZEN"}
	a, err := svc.FreezeAccount(context.Background(), admin, target, "fraud ring suspected", "")
	var de *DownstreamError
	if !errors.As(err, &de) || de.Status != 409 {
		t.Fatalf("err = %v", err)
	}
	if rec := repo.actions[a.ID]; rec.Status != StatusFailed || rec.FailureReason != "409 ACCOUNT_ALREADY_FROZEN" {
		t.Errorf("record = %+v", rec)
	}
}

func TestOutcomeNotRecordedLeavesActionRequested(t *testing.T) {
	svc, repo, _, _, _, _ := setup()
	repo.failFinish = true
	a, err := svc.FreezeAccount(context.Background(), admin, target, "fraud ring suspected", "")
	if err != nil {
		t.Fatalf("the downstream action succeeded, err = %v", err)
	}
	if repo.actions[a.ID].Status != StatusRequested || a.Status != StatusRequested || a.ResolvedAt != nil {
		t.Error("an unrecorded outcome must stay visibly REQUESTED")
	}
}

func TestValidationRejectsBeforeAnythingIsRecorded(t *testing.T) {
	svc, repo, acc, _, _, _ := setup()
	ctx := context.Background()
	cases := map[string]func() error{
		"short reason": func() error { _, e := svc.FreezeAccount(ctx, admin, target, "no", ""); return e },
		"blank reason": func() error { _, e := svc.FreezeAccount(ctx, admin, target, "      ", ""); return e },
		"long reason": func() error {
			_, e := svc.FreezeAccount(ctx, admin, target, strings.Repeat("a", MaxReasonRunes+1), "")
			return e
		},
		"control chars":  func() error { _, e := svc.FreezeAccount(ctx, admin, target, "bad\x00reason", ""); return e },
		"bad account id": func() error { _, e := svc.FreezeAccount(ctx, admin, "../etc", "valid reason", ""); return e },
		"bad outcome": func() error {
			_, e := svc.ResolveFraudCase(ctx, admin, "tok", target, "MAYBE", "valid reason", "")
			return e
		},
		"approve tier 0": func() error { _, e := svc.ReviewKYC(ctx, admin, "tok", target, true, 0, "valid reason", ""); return e },
		"approve tier 9": func() error { _, e := svc.ReviewKYC(ctx, admin, "tok", target, true, 9, "valid reason", ""); return e },
	}
	for name, run := range cases {
		if run() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if len(repo.actions) != 0 || acc.calls != 0 {
		t.Error("an invalid request must leave no record and call nothing")
	}
}

func TestFraudResolveForwardsTheAdminsOwnToken(t *testing.T) {
	svc, repo, _, fr, _, _ := setup()
	a, err := svc.ResolveFraudCase(context.Background(), admin, "the-admins-jwt", target, "CONFIRMED", "reviewed with customer", "")
	if err != nil || fr.gotBearer != "the-admins-jwt" {
		t.Fatalf("err=%v bearer=%q", err, fr.gotBearer)
	}
	if !strings.Contains(string(repo.actions[a.ID].Params), `"CONFIRMED"`) {
		t.Errorf("params = %s", repo.actions[a.ID].Params)
	}
}

func TestRejectedKYCZeroesTheTier(t *testing.T) {
	svc, _, _, _, cu, _ := setup()
	if _, err := svc.ReviewKYC(context.Background(), admin, "tok", target, false, 3, "documents unreadable", ""); err != nil {
		t.Fatal(err)
	}
	if cu.approve || cu.tier != 0 {
		t.Errorf("approve=%v tier=%d", cu.approve, cu.tier)
	}
}

type fakeMetrics struct{ fail bool }

func (f fakeMetrics) Scalar(context.Context, string) (float64, error) {
	if f.fail {
		return 0, errors.New("prometheus down")
	}
	return 4, nil
}
func (f fakeMetrics) Firing(context.Context) ([]string, error) { return nil, nil }

func TestDashboardDegradesPerSection(t *testing.T) {
	svc, _, _, _, _, _ := setup()
	d := svc.Dashboard(context.Background(), "tok", fakeMetrics{fail: true})
	if !d.Fraud.Available {
		t.Error("fraud panel does not depend on Prometheus")
	}
	if d.Payments.Available || d.Ops.Available {
		t.Error("panels fed by a failing Prometheus must be marked unavailable, not zero")
	}
	d = svc.Dashboard(context.Background(), "tok", fakeMetrics{})
	if !d.Payments.Available || d.Payments.Data.Completed1h != 4 || d.Ops.Data.FiringAlerts == nil {
		t.Errorf("dashboard = %+v", d)
	}
}
