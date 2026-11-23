package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/finverse/shared/eventbus"
	"github.com/google/uuid"
)

type fakeRepo struct {
	byEvent map[string]Record
	err     error
}

func (f *fakeRepo) Append(_ context.Context, r Record) error {
	if f.err != nil {
		return f.err
	}
	if _, dup := f.byEvent[r.EventID]; dup {
		return nil // idempotent, like ON CONFLICT DO NOTHING
	}
	f.byEvent[r.EventID] = r
	return nil
}
func (f *fakeRepo) List(context.Context, Query) ([]Record, error) { return nil, nil }

func envelope() eventbus.Envelope {
	return eventbus.Envelope{EventID: uuid.NewString(), EventType: "PaymentCompleted", SourceService: "payment-service",
		OccurredAt: time.Now(), Data: json.RawMessage(`{"paymentId":"p1"}`)}
}

func TestHandleRecordsOnceEvenIfRedelivered(t *testing.T) {
	repo := &fakeRepo{byEvent: map[string]Record{}}
	svc := New(repo)
	env := envelope()
	for i := 0; i < 3; i++ {
		if err := svc.Handle(context.Background(), env); err != nil {
			t.Fatal(err)
		}
	}
	if len(repo.byEvent) != 1 {
		t.Fatalf("got %d records for one event, want 1", len(repo.byEvent))
	}
	if string(repo.byEvent[env.EventID].Payload) != `{"paymentId":"p1"}` {
		t.Error("payload not preserved verbatim")
	}
}

func TestHandleDatabaseFailureIsRetryable(t *testing.T) {
	err := New(&fakeRepo{byEvent: map[string]Record{}, err: errors.New("connection reset")}).Handle(context.Background(), envelope())
	if err == nil || eventbus.IsPermanent(err) {
		t.Fatalf("want a retryable error, got %v", err)
	}
}

func TestHandleUnrecordableEnvelopesArePermanent(t *testing.T) {
	svc := New(&fakeRepo{byEvent: map[string]Record{}})
	bad := envelope()
	bad.EventID = "not-a-uuid"
	badData := envelope()
	badData.Data = json.RawMessage(`{broken`)
	for name, env := range map[string]eventbus.Envelope{"bad id": bad, "bad json": badData} {
		if err := svc.Handle(context.Background(), env); !eventbus.IsPermanent(err) {
			t.Errorf("%s: want Permanent, got %v", name, err)
		}
	}
}

func TestListClampsLimit(t *testing.T) {
	var got int
	repo := &limitRepo{fn: func(q Query) { got = q.Limit }}
	_, _ = New(repo).List(context.Background(), Query{Limit: 100000})
	if got != 50 {
		t.Errorf("limit = %d, want clamp to 50", got)
	}
}

type limitRepo struct{ fn func(Query) }

func (l *limitRepo) Append(context.Context, Record) error { return nil }
func (l *limitRepo) List(_ context.Context, q Query) ([]Record, error) {
	l.fn(q)
	return nil, nil
}
