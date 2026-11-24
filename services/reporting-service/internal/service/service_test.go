package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type memRepo struct {
	reports map[string]*Report
}

func (m *memRepo) Create(_ context.Context, r Report) error {
	cp := r
	m.reports[r.ID] = &cp
	return nil
}
func (m *memRepo) CountActive(_ context.Context, who string) (int, error) {
	n := 0
	for _, r := range m.reports {
		if r.RequesterID == who && (r.Status == StatusQueued || r.Status == StatusRunning) {
			n++
		}
	}
	return n, nil
}
func (m *memRepo) Get(_ context.Context, id string) (*Report, error) {
	r, ok := m.reports[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *r
	return &cp, nil
}
func (m *memRepo) ListByRequester(context.Context, string, int) ([]Report, error) { return nil, nil }
func (m *memRepo) Claim(_ context.Context, _ time.Time, _ time.Duration) (*Report, error) {
	for _, r := range m.reports {
		if r.Status == StatusQueued {
			r.Status = StatusRunning
			r.Attempts++
			cp := *r
			return &cp, nil
		}
	}
	return nil, nil
}
func (m *memRepo) Complete(_ context.Context, id string, rows int, size int64, sum, key string, now, exp time.Time) error {
	r := m.reports[id]
	r.Status, r.RowCount, r.SizeBytes, r.SHA256, r.FileKey, r.ExpiresAt = StatusReady, rows, size, sum, key, &exp
	return nil
}
func (m *memRepo) Fail(_ context.Context, id, code string, permanent bool, _ time.Time) error {
	r := m.reports[id]
	r.FailureCode = code
	if permanent || r.Attempts >= MaxAttempts {
		r.Status = StatusFailed
	} else {
		r.Status = StatusQueued
	}
	return nil
}
func (m *memRepo) ExpireDue(context.Context, time.Time) ([]string, error) { return nil, nil }

type memStore struct{ files map[string][]byte }

func (s *memStore) Put(_ context.Context, k string, d []byte) error { s.files[k] = d; return nil }
func (s *memStore) Get(_ context.Context, k string) ([]byte, error) {
	d, ok := s.files[k]
	if !ok {
		return nil, errors.New("missing")
	}
	return d, nil
}
func (s *memStore) Delete(_ context.Context, k string) error { delete(s.files, k); return nil }

// fakeLedger serves a fixed set of legs newest first, honouring from/to and paging.
type fakeLedger struct {
	legs    []Entry
	err     error
	calls   int
	maxPage int
}

func (f *fakeLedger) History(_ context.Context, _ string, from, to *time.Time, limit int, cursor string) ([]Entry, string, error) {
	f.calls++
	if f.err != nil {
		return nil, "", f.err
	}
	var in []Entry
	for _, e := range f.legs {
		if (from != nil && e.At.Before(*from)) || (to != nil && e.At.After(*to)) {
			continue
		}
		in = append(in, e)
	}
	start := 0
	if cursor != "" {
		for i, e := range in {
			if e.LegID == cursor {
				start = i + 1
			}
		}
	}
	end := start + limit
	if f.maxPage > 0 && end > start+f.maxPage {
		end = start + f.maxPage
	}
	if end >= len(in) {
		return in[start:], "", nil
	}
	return in[start:end], in[end-1].LegID, nil
}

type fakeAccounts struct {
	allowed bool
	err     error
}

func (f fakeAccounts) CanAccess(context.Context, string, string) (bool, error) {
	return f.allowed, f.err
}

type fakeFraud struct {
	cases []FraudCase
	err   error
}

func (f fakeFraud) OpenAndResolvedCases(context.Context, string) ([]FraudCase, error) {
	return f.cases, f.err
}

var (
	owner   = uuid.NewString()
	account = uuid.NewString()
	now     = time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
)

func newSvc(led *fakeLedger, acc fakeAccounts) (*Service, *memRepo, *memStore) {
	repo, store := &memRepo{reports: map[string]*Report{}}, &memStore{files: map[string][]byte{}}
	s := New(repo, store, led, acc, fakeFraud{}, nil)
	s.now = func() time.Time { return now }
	return s, repo, store
}

func leg(id string, at time.Time, amt int64, dir string) Entry {
	return Entry{TransactionID: "t-" + id, LegID: id, AmountMinor: amt, Direction: dir, Currency: "INR", At: at}
}

func TestParseRange(t *testing.T) {
	ok := [][2]string{{"2026-01-01", "2026-01-31"}, {"2025-03-15", "2026-03-15"}, {"2026-03-15", "2026-03-15"}}
	for _, r := range ok {
		if _, _, err := ParseRange(r[0], r[1], now); err != nil {
			t.Errorf("%v rejected: %v", r, err)
		}
	}
	bad := map[string][2]string{
		"not dates":     {"yesterday", "today"},
		"reversed":      {"2026-02-01", "2026-01-01"},
		"future":        {"2026-03-01", "2026-03-16"},
		"367 days":      {"2025-03-14", "2026-03-15"},
		"ancient":       {"1999-01-01", "1999-02-01"},
		"datetime form": {"2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z"},
	}
	for name, r := range bad {
		if _, _, err := ParseRange(r[0], r[1], now); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s accepted (err=%v)", name, err)
		}
	}
}

func TestStatementEndToEnd(t *testing.T) {
	jan := func(d, h int) time.Time { return time.Date(2026, 1, d, h, 0, 0, 0, time.UTC) }
	led := &fakeLedger{legs: []Entry{ // newest first
		leg("l5", jan(31, 23), 100, "D"), // last hour of the range: must be included
		leg("l4", jan(10, 9), 5000, "C"),
		leg("l3", jan(1, 0), 200, "D"),                                       // first instant of the range: included
		leg("l2", time.Date(2025, 12, 31, 23, 0, 0, 0, time.UTC), 1000, "C"), // before: opening only
		leg("l1", time.Date(2025, 11, 1, 0, 0, 0, 0, time.UTC), 300, "C"),
		leg("l0", time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC), 50, "D"),
	}, maxPage: 2} // force multiple pages for both the opening and range queries
	s, repo, _ := newSvc(led, fakeAccounts{allowed: true})

	r, err := s.RequestStatement(context.Background(), owner, "tok", account, "2026-01-01", "2026-01-31")
	if err != nil {
		t.Fatal(err)
	}
	if did, err := s.ProcessNext(context.Background()); !did || err != nil {
		t.Fatalf("ProcessNext = %v, %v", did, err)
	}
	if repo.reports[r.ID].Status != StatusReady {
		t.Fatalf("status = %s (%s)", repo.reports[r.ID].Status, repo.reports[r.ID].FailureCode)
	}
	data, name, err := s.Download(context.Background(), owner, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if name != "statement-2026-01-01-to-2026-01-31.csv" {
		t.Errorf("filename = %q", name)
	}
	got := string(data)
	// opening = +1000 +300 -50 = 1250 (12.50); closing = 1250 -200 +5000 -100 = 5950 (59.50)
	for _, want := range []string{"OPENING_BALANCE,,,12.50,INR", "t-l3,TRANSACTION,2.00,,10.50", "t-l4,TRANSACTION,,50.00,60.50", "t-l5,TRANSACTION,1.00,,59.50", "CLOSING_BALANCE,,,59.50,INR"} {
		if !strings.Contains(got, want) {
			t.Errorf("statement lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "t-l2") || strings.Contains(got, "t-l1") {
		t.Error("legs before the range must not be listed")
	}
	if repo.reports[r.ID].RowCount != 3 {
		t.Errorf("row count = %d, want 3", repo.reports[r.ID].RowCount)
	}
}

func TestOnlyHoldersCanRequestAndNothingIsQueuedForOthers(t *testing.T) {
	s, repo, _ := newSvc(&fakeLedger{}, fakeAccounts{allowed: false})
	_, err := s.RequestStatement(context.Background(), owner, "tok", account, "2026-01-01", "2026-01-31")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound (same answer as a missing account)", err)
	}
	if len(repo.reports) != 0 {
		t.Error("a request for someone else's account must not be queued")
	}
	s, _, _ = newSvc(&fakeLedger{}, fakeAccounts{err: errors.New("account-service down")})
	if _, err := s.RequestStatement(context.Background(), owner, "tok", account, "2026-01-01", "2026-01-31"); !errors.Is(err, ErrUpstream) {
		t.Errorf("ownership unknown must fail closed with ErrUpstream, got %v", err)
	}
}

func TestOnlyTheRequesterCanSeeOrDownload(t *testing.T) {
	s, _, _ := newSvc(&fakeLedger{}, fakeAccounts{allowed: true})
	r, _ := s.RequestStatement(context.Background(), owner, "tok", account, "2026-01-01", "2026-01-31")
	_, _ = s.ProcessNext(context.Background())
	stranger := uuid.NewString()
	if _, err := s.Get(context.Background(), stranger, r.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("stranger Get: %v", err)
	}
	if _, _, err := s.Download(context.Background(), stranger, r.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("stranger Download: %v", err)
	}
	if _, err := s.Get(context.Background(), owner, "../../etc/passwd"); !errors.Is(err, ErrNotFound) {
		t.Errorf("malformed id: %v", err)
	}
}

func TestActiveReportCap(t *testing.T) {
	s, _, _ := newSvc(&fakeLedger{}, fakeAccounts{allowed: true})
	for i := 0; i < MaxActive; i++ {
		if _, err := s.RequestStatement(context.Background(), owner, "tok", account, "2026-01-01", "2026-01-31"); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	if _, err := s.RequestStatement(context.Background(), owner, "tok", account, "2026-01-01", "2026-01-31"); !errors.Is(err, ErrTooManyActive) {
		t.Errorf("err = %v, want ErrTooManyActive", err)
	}
	if _, err := s.RequestStatement(context.Background(), uuid.NewString(), "tok", account, "2026-01-01", "2026-01-31"); err != nil {
		t.Errorf("the cap is per requester: %v", err)
	}
}

func TestDownloadBeforeReady(t *testing.T) {
	s, _, _ := newSvc(&fakeLedger{}, fakeAccounts{allowed: true})
	r, _ := s.RequestStatement(context.Background(), owner, "tok", account, "2026-01-01", "2026-01-31")
	if _, _, err := s.Download(context.Background(), owner, r.ID); !errors.Is(err, ErrNotReady) {
		t.Errorf("err = %v", err)
	}
}

func TestTamperedFileIsRefused(t *testing.T) {
	s, _, store := newSvc(&fakeLedger{}, fakeAccounts{allowed: true})
	r, _ := s.RequestStatement(context.Background(), owner, "tok", account, "2026-01-01", "2026-01-31")
	_, _ = s.ProcessNext(context.Background())
	store.files[r.ID+".csv"] = []byte("date,altered\r\n")
	if _, _, err := s.Download(context.Background(), owner, r.ID); err == nil {
		t.Error("a file whose bytes changed after generation must not be served")
	}
}

func TestTransientFailureRetriesThenFails(t *testing.T) {
	led := &fakeLedger{err: errors.New("transaction-service unavailable")}
	s, repo, _ := newSvc(led, fakeAccounts{allowed: true})
	r, _ := s.RequestStatement(context.Background(), owner, "tok", account, "2026-01-01", "2026-01-31")
	for i := 1; i <= MaxAttempts; i++ {
		if did, _ := s.ProcessNext(context.Background()); !did {
			t.Fatalf("attempt %d: nothing claimed", i)
		}
		want := StatusQueued
		if i == MaxAttempts {
			want = StatusFailed
		}
		if got := repo.reports[r.ID].Status; got != want {
			t.Fatalf("after attempt %d status = %s, want %s", i, got, want)
		}
	}
	if repo.reports[r.ID].FailureCode != "GENERATION_FAILED" {
		t.Errorf("code = %s", repo.reports[r.ID].FailureCode)
	}
	if did, _ := s.ProcessNext(context.Background()); did {
		t.Error("a failed report must not be picked up again")
	}
}

func TestTooLargeIsPermanentImmediately(t *testing.T) {
	var legs []Entry
	for i := 0; i < MaxRows+1; i++ {
		legs = append(legs, leg(uuid.NewString(), time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), 1, "C"))
	}
	s, repo, _ := newSvc(&fakeLedger{legs: legs}, fakeAccounts{allowed: true})
	r, _ := s.RequestStatement(context.Background(), owner, "tok", account, "2026-01-01", "2026-01-31")
	_, _ = s.ProcessNext(context.Background())
	if got := repo.reports[r.ID]; got.Status != StatusFailed || got.FailureCode != "TOO_LARGE" || got.Attempts != 1 {
		t.Errorf("report = %+v", got)
	}
}

func TestFraudSummaryUsesSnapshotTakenAtRequestTime(t *testing.T) {
	repo, store := &memRepo{reports: map[string]*Report{}}, &memStore{files: map[string][]byte{}}
	s := New(repo, store, &fakeLedger{err: errors.New("must not be called")}, fakeAccounts{},
		fakeFraud{cases: []FraudCase{{ID: "c1", AmountMinor: 12345, Status: "OPEN", Verdict: "REVIEW"}}}, nil)
	r, err := s.RequestFraudSummary(context.Background(), owner, "admin-token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProcessNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, name, err := s.Download(context.Background(), owner, r.ID)
	if err != nil || !strings.Contains(string(data), "c1") || !strings.Contains(string(data), "123.45") || !strings.HasPrefix(name, "fraud-summary-") {
		t.Fatalf("err=%v name=%s data=%s", err, name, data)
	}

	s = New(repo, store, nil, fakeAccounts{}, fakeFraud{err: errors.New("fraud down")}, nil)
	if _, err := s.RequestFraudSummary(context.Background(), owner, "admin-token"); !errors.Is(err, ErrUpstream) {
		t.Errorf("err = %v", err)
	}
}
