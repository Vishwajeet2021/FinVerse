package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/finverse/notification-service/internal/sender"
	"github.com/finverse/shared/eventbus"
)

type fakeRepo struct {
	byEvent  map[string]*Notification
	attempts map[string]int
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{byEvent: map[string]*Notification{}, attempts: map[string]int{}}
}

func (f *fakeRepo) Create(_ context.Context, n Notification, _ string) (Notification, error) {
	if ex, ok := f.byEvent[n.EventID]; ok {
		return *ex, nil
	}
	n.ID = "n-" + n.EventID
	n.Status = "PENDING"
	f.byEvent[n.EventID] = &n
	return n, nil
}
func (f *fakeRepo) RecordAttempt(_ context.Context, id string, _ error) error {
	f.attempts[id]++
	return nil
}
func (f *fakeRepo) MarkSent(_ context.Context, id string) error {
	for _, n := range f.byEvent {
		if n.ID == id {
			n.Status = "SENT"
		}
	}
	return nil
}
func (f *fakeRepo) ListByRecipient(context.Context, string, int) ([]Notification, error) {
	return nil, nil
}
func (f *fakeRepo) ListAll(context.Context, int) ([]Notification, error) { return nil, nil }

type countingSender struct {
	err   error
	calls int
}

func (c *countingSender) Send(context.Context, sender.Message) error { c.calls++; return c.err }

func event(typ string, data any) eventbus.Envelope {
	b, _ := json.Marshal(data)
	return eventbus.Envelope{EventID: "evt-1", EventType: typ, Data: b}
}

func TestHandleDeliversOnce(t *testing.T) {
	repo, snd := newFakeRepo(), &countingSender{}
	svc := New(repo, snd)
	env := event("PaymentCompleted", map[string]string{"paymentId": "p1", "transactionId": "t1"})

	if err := svc.Handle(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	// A redelivery after a lost ack must not send a second email.
	if err := svc.Handle(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	if snd.calls != 1 {
		t.Errorf("sender called %d times, want 1", snd.calls)
	}
}

func TestHandleProviderFailureIsRetryableNotPermanent(t *testing.T) {
	svc := New(newFakeRepo(), &countingSender{err: sender.ErrProviderDown})
	err := svc.Handle(context.Background(), event("PaymentFailed", map[string]string{"paymentId": "p1", "reason": "x"}))
	if err == nil || eventbus.IsPermanent(err) || !errors.Is(err, sender.ErrProviderDown) {
		t.Fatalf("want a retryable provider error, got %v (permanent=%v)", err, eventbus.IsPermanent(err))
	}
}

func TestHandleProviderRecoversOnRetry(t *testing.T) {
	repo, snd := newFakeRepo(), &countingSender{err: sender.ErrProviderDown}
	svc := New(repo, snd)
	env := event("AccountOpened", map[string]string{"accountId": "a", "customerId": "c1", "accountNumber": "123"})

	if err := svc.Handle(context.Background(), env); err == nil {
		t.Fatal("expected failure during outage")
	}
	snd.err = nil
	if err := svc.Handle(context.Background(), env); err != nil {
		t.Fatalf("retry after recovery: %v", err)
	}
	if got := repo.byEvent["evt-1"].Status; got != "SENT" {
		t.Errorf("status = %s, want SENT", got)
	}
}

func TestPoisonMessagesArePermanent(t *testing.T) {
	svc := New(newFakeRepo(), &countingSender{})
	cases := map[string]eventbus.Envelope{
		"unknown type":  event("Mystery", map[string]string{}),
		"missing field": event("PaymentCompleted", map[string]string{}),
		"unparseable":   {EventID: "e", EventType: "PaymentFailed", Data: json.RawMessage(`"not an object"`)},
	}
	for name, env := range cases {
		if err := svc.Handle(context.Background(), env); !eventbus.IsPermanent(err) {
			t.Errorf("%s: want Permanent error, got %v", name, err)
		}
	}
}
