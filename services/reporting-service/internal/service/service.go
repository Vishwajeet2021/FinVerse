// Package service is Reporting Service's use-case layer (docs/23 units D).
//
// Every report is generated asynchronously (D-R3): the API validates, authorises and queues a
// request; a leased worker builds the file; the owner downloads it through the authenticated
// API (D-R2). Statements are built from Transaction Service's ledger history, the system of
// record (D-R4), never from a projection.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Kind string

const (
	KindStatement    Kind = "STATEMENT"
	KindFraudSummary Kind = "FRAUD_SUMMARY"
)

type Status string

const (
	StatusQueued  Status = "QUEUED"
	StatusRunning Status = "RUNNING"
	StatusReady   Status = "READY"
	StatusFailed  Status = "FAILED"
	StatusExpired Status = "EXPIRED"
)

const (
	MaxRangeDays   = 366
	MaxRows        = 50_000 // rows inside the requested range
	MaxOpeningLegs = 100_000
	PageSize       = 200
	MaxActive      = 5 // queued or running reports per requester
	MaxAttempts    = 3
	Retention      = 7 * 24 * time.Hour
	Lease          = 2 * time.Minute
	dateLayout     = "2006-01-02"
)

type Report struct {
	ID          string     `json:"id"`
	RequesterID string     `json:"-"`
	Kind        Kind       `json:"kind"`
	AccountID   string     `json:"accountId,omitempty"`
	From        *time.Time `json:"from,omitempty"`
	To          *time.Time `json:"to,omitempty"`
	Status      Status     `json:"status"`
	RowCount    int        `json:"rowCount"`
	SizeBytes   int64      `json:"sizeBytes"`
	SHA256      string     `json:"sha256,omitempty"`
	FileKey     string     `json:"-"`
	FailureCode string     `json:"failureCode,omitempty"`
	Attempts    int        `json:"-"`
	Snapshot    []byte     `json:"-"` // input captured at request time for reports that read other services
	CreatedAt   time.Time  `json:"createdAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
}

var (
	ErrNotFound      = errors.New("report not found")
	ErrNotReady      = errors.New("report is not ready")
	ErrInvalidInput  = errors.New("invalid input")
	ErrTooManyActive = errors.New("too many reports in progress")
	ErrTooLarge      = errors.New("report exceeds the size limits")
	ErrUpstream      = errors.New("upstream service unavailable")
)

type Repository interface {
	Create(ctx context.Context, r Report) error
	CountActive(ctx context.Context, requesterID string) (int, error)
	Get(ctx context.Context, id string) (*Report, error)
	ListByRequester(ctx context.Context, requesterID string, limit int) ([]Report, error)
	// Claim atomically takes the oldest QUEUED report, or a RUNNING one whose lease expired
	// (a crashed worker), and leases it. It returns nil, nil when there is nothing to do.
	Claim(ctx context.Context, now time.Time, lease time.Duration) (*Report, error)
	Complete(ctx context.Context, id string, rows int, size int64, sum, fileKey string, now time.Time, expires time.Time) error
	// Fail records a failure. A permanent failure ends the report; otherwise it is requeued
	// until MaxAttempts is reached.
	Fail(ctx context.Context, id, code string, permanent bool, now time.Time) error
	// ExpireDue marks READY reports past their expiry as EXPIRED and returns their file keys.
	ExpireDue(ctx context.Context, now time.Time) ([]string, error)
}

type Storage interface {
	Put(ctx context.Context, key string, data []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
}

type Ledger interface {
	// History returns legs newest first, like the ledger does.
	History(ctx context.Context, accountID string, from, to *time.Time, limit int, cursor string) ([]Entry, string, error)
}

type Accounts interface {
	// CanAccess reports whether the bearer's owner may read the account. A non-holder gets false, nil.
	CanAccess(ctx context.Context, bearer, accountID string) (bool, error)
}

type Fraud interface {
	OpenAndResolvedCases(ctx context.Context, bearer string) ([]FraudCase, error)
}

type Service struct {
	repo     Repository
	store    Storage
	ledger   Ledger
	accounts Accounts
	fraud    Fraud
	now      func() time.Time
	logf     func(msg string, kv ...any)
}

func New(repo Repository, store Storage, ledger Ledger, accounts Accounts, fraud Fraud, logf func(string, ...any)) *Service {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Service{repo: repo, store: store, ledger: ledger, accounts: accounts, fraud: fraud, now: time.Now, logf: logf}
}

// ParseRange validates a statement period given as calendar dates in UTC.
func ParseRange(fromS, toS string, now time.Time) (from, to time.Time, err error) {
	from, e1 := time.Parse(dateLayout, fromS)
	to, e2 := time.Parse(dateLayout, toS)
	if e1 != nil || e2 != nil {
		return from, to, fmt.Errorf("%w: dates must be YYYY-MM-DD", ErrInvalidInput)
	}
	today := now.UTC().Truncate(24 * time.Hour)
	switch {
	case to.Before(from):
		return from, to, fmt.Errorf("%w: from must not be after to", ErrInvalidInput)
	case to.After(today):
		return from, to, fmt.Errorf("%w: to must not be in the future", ErrInvalidInput)
	case int(to.Sub(from).Hours()/24)+1 > MaxRangeDays:
		return from, to, fmt.Errorf("%w: a statement covers at most %d days", ErrInvalidInput, MaxRangeDays)
	case from.Year() < 2000:
		return from, to, fmt.Errorf("%w: from is too far in the past", ErrInvalidInput)
	}
	return from, to, nil
}

func (s *Service) admit(ctx context.Context, requesterID string) error {
	n, err := s.repo.CountActive(ctx, requesterID)
	if err != nil {
		return err
	}
	if n >= MaxActive {
		return ErrTooManyActive
	}
	return nil
}

// RequestStatement queues a statement for an account the caller holds. A caller who does not
// hold the account gets ErrNotFound, the same as for an account that does not exist, so ids
// cannot be probed (D-R5).
func (s *Service) RequestStatement(ctx context.Context, requesterID, bearer, accountID, fromS, toS string) (*Report, error) {
	if _, err := uuid.Parse(accountID); err != nil {
		return nil, fmt.Errorf("%w: accountId is not valid", ErrInvalidInput)
	}
	from, to, err := ParseRange(fromS, toS, s.now())
	if err != nil {
		return nil, err
	}
	ok, err := s.accounts.CanAccess(ctx, bearer, accountID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	if !ok {
		return nil, ErrNotFound
	}
	if err := s.admit(ctx, requesterID); err != nil {
		return nil, err
	}
	r := Report{ID: uuid.NewString(), RequesterID: requesterID, Kind: KindStatement, AccountID: accountID,
		From: &from, To: &to, Status: StatusQueued, CreatedAt: s.now().UTC()}
	if err := s.repo.Create(ctx, r); err != nil {
		return nil, err
	}
	return &r, nil
}

// RequestFraudSummary queues the operational fraud report. The case list is read now, with the
// administrator's own token (the Fraud API enforces fraud:read), and stored with the request;
// the worker renders that snapshot and needs no credentials of its own.
func (s *Service) RequestFraudSummary(ctx context.Context, requesterID, bearer string) (*Report, error) {
	if err := s.admit(ctx, requesterID); err != nil {
		return nil, err
	}
	cases, err := s.fraud.OpenAndResolvedCases(ctx, bearer)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	snap, err := json.Marshal(cases)
	if err != nil {
		return nil, err
	}
	r := Report{ID: uuid.NewString(), RequesterID: requesterID, Kind: KindFraudSummary, Status: StatusQueued,
		Snapshot: snap, CreatedAt: s.now().UTC()}
	if err := s.repo.Create(ctx, r); err != nil {
		return nil, err
	}
	return &r, nil
}

// Get returns a report only to its requester; anyone else sees ErrNotFound.
func (s *Service) Get(ctx context.Context, requesterID, id string) (*Report, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, ErrNotFound
	}
	r, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if r.RequesterID != requesterID {
		return nil, ErrNotFound
	}
	return r, nil
}

func (s *Service) List(ctx context.Context, requesterID string, limit int) ([]Report, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return s.repo.ListByRequester(ctx, requesterID, limit)
}

// Download returns the file and a server-generated filename (never client supplied, D-R2).
func (s *Service) Download(ctx context.Context, requesterID, id string) ([]byte, string, error) {
	r, err := s.Get(ctx, requesterID, id)
	if err != nil {
		return nil, "", err
	}
	if r.Status != StatusReady {
		return nil, "", ErrNotReady
	}
	data, err := s.store.Get(ctx, r.FileKey)
	if err != nil {
		return nil, "", fmt.Errorf("reading stored report: %w", err)
	}
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != r.SHA256 {
		// Stored bytes no longer match what was generated. Refuse rather than hand out a file
		// that may have been altered.
		return nil, "", fmt.Errorf("stored report failed its integrity check")
	}
	name := "fraud-summary-" + r.CreatedAt.UTC().Format("20060102") + ".csv"
	if r.Kind == KindStatement && r.From != nil && r.To != nil {
		name = "statement-" + r.From.Format(dateLayout) + "-to-" + r.To.Format(dateLayout) + ".csv"
	}
	return data, name, nil
}

// ProcessNext claims and builds one report. It returns false when the queue is empty.
func (s *Service) ProcessNext(ctx context.Context) (bool, error) {
	r, err := s.repo.Claim(ctx, s.now(), Lease)
	if err != nil {
		return false, err
	}
	if r == nil {
		return false, nil
	}
	data, rows, buildErr := s.build(ctx, r)
	now := s.now().UTC()
	if buildErr != nil {
		code, permanent := failureCode(buildErr)
		s.logf("report generation failed", "report_id", r.ID, "kind", r.Kind, "code", code, "permanent", permanent, "error", buildErr)
		if err := s.repo.Fail(ctx, r.ID, code, permanent, now); err != nil {
			return true, err
		}
		return true, nil
	}
	key := r.ID + ".csv"
	if err := s.store.Put(ctx, key, data); err != nil {
		s.logf("storing report failed", "report_id", r.ID, "error", err)
		return true, s.repo.Fail(ctx, r.ID, "STORAGE_FAILED", false, now)
	}
	sum := sha256.Sum256(data)
	if err := s.repo.Complete(ctx, r.ID, rows, int64(len(data)), hex.EncodeToString(sum[:]), key, now, now.Add(Retention)); err != nil {
		_ = s.store.Delete(ctx, key) // never leave a file no row points to
		return true, err
	}
	return true, nil
}

func failureCode(err error) (code string, permanent bool) {
	switch {
	case errors.Is(err, ErrTooLarge):
		return "TOO_LARGE", true
	case errors.Is(err, ErrMixedCurrency):
		return "MIXED_CURRENCY", true
	case errors.Is(err, ErrBadEntry):
		return "BAD_LEDGER_DATA", true
	default:
		return "GENERATION_FAILED", false // transient until attempts run out
	}
}

func (s *Service) build(ctx context.Context, r *Report) (data []byte, rows int, err error) {
	switch r.Kind {
	case KindFraudSummary:
		var cases []FraudCase
		if err := json.Unmarshal(r.Snapshot, &cases); err != nil {
			return nil, 0, fmt.Errorf("%w: snapshot unreadable", ErrBadEntry)
		}
		data, err = BuildFraudSummary(cases)
		return data, len(cases), err
	case KindStatement:
		return s.buildStatement(ctx, r)
	}
	return nil, 0, fmt.Errorf("%w: unknown kind %q", ErrBadEntry, r.Kind)
}

func signed(e Entry) int64 {
	if e.Direction == "C" {
		return e.AmountMinor
	}
	return -e.AmountMinor
}

func (s *Service) buildStatement(ctx context.Context, r *Report) ([]byte, int, error) {
	from := r.From.UTC()
	to := r.To.UTC().Add(24*time.Hour - time.Microsecond) // the whole last day (Postgres keeps microseconds)

	// Opening balance: everything before the first instant of the range.
	var opening int64
	before := from.Add(-time.Microsecond)
	cursor, seen := "", 0
	for {
		page, next, err := s.ledger.History(ctx, r.AccountID, nil, &before, PageSize, cursor)
		if err != nil {
			return nil, 0, err
		}
		for _, e := range page {
			opening += signed(e)
		}
		if seen += len(page); seen > MaxOpeningLegs {
			return nil, 0, fmt.Errorf("%w: more than %d legs before the start date", ErrTooLarge, MaxOpeningLegs)
		}
		if next == "" {
			break
		}
		cursor = next
	}

	var entries []Entry
	cursor = ""
	for {
		page, next, err := s.ledger.History(ctx, r.AccountID, &from, &to, PageSize, cursor)
		if err != nil {
			return nil, 0, err
		}
		entries = append(entries, page...)
		if len(entries) > MaxRows {
			return nil, 0, fmt.Errorf("%w: more than %d rows in range", ErrTooLarge, MaxRows)
		}
		if next == "" {
			break
		}
		cursor = next
	}
	data, _, err := BuildStatement(opening, entries, from, r.To.UTC().Add(24*time.Hour-time.Second))
	return data, len(entries), err
}

// Sweep removes expired report files. Returns how many were removed.
func (s *Service) Sweep(ctx context.Context) (int, error) {
	keys, err := s.repo.ExpireDue(ctx, s.now())
	if err != nil {
		return 0, err
	}
	for _, k := range keys {
		if err := s.store.Delete(ctx, k); err != nil {
			s.logf("deleting expired report file failed", "key", k, "error", err)
		}
	}
	return len(keys), nil
}
