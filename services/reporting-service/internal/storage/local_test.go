package storage

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
)

func TestRoundTripAndDelete(t *testing.T) {
	l, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, key := context.Background(), uuid.NewString()+".csv"
	if err := l.Put(ctx, key, []byte("a,b\r\n")); err != nil {
		t.Fatal(err)
	}
	if got, err := l.Get(ctx, key); err != nil || string(got) != "a,b\r\n" {
		t.Fatalf("got %q, %v", got, err)
	}
	if err := l.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Get(ctx, key); !os.IsNotExist(err) {
		t.Errorf("deleted file still readable: %v", err)
	}
	if err := l.Delete(ctx, key); err != nil {
		t.Errorf("deleting twice must be harmless: %v", err)
	}
}

func TestKeysThatAreNotOurOwnAreRefused(t *testing.T) {
	l, _ := NewLocal(t.TempDir())
	for _, k := range []string{"../../etc/passwd", "a.csv", "", "/abs.csv", uuid.NewString(), uuid.NewString() + ".csv/..", uuid.NewString() + ".exe", "..\\x.csv"} {
		if _, err := l.path(k); err != ErrBadKey {
			t.Errorf("key %q accepted", k)
		}
		if err := l.Put(context.Background(), k, nil); err != ErrBadKey {
			t.Errorf("Put %q: %v", k, err)
		}
	}
}
