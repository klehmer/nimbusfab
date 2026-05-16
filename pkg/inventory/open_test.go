package inventory

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// resetOpeners restores openers around each test so register calls don't
// leak between cases.
func snapshotOpeners(t *testing.T) {
	t.Helper()
	prev := map[string]Opener{}
	for k, v := range openers {
		prev[k] = v
	}
	t.Cleanup(func() {
		openers = prev
	})
}

func TestOpen_UnknownScheme(t *testing.T) {
	snapshotOpeners(t)
	openers = map[string]Opener{}
	_, err := Open(context.Background(), "mysql://host/db")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "mysql") {
		t.Errorf("error should mention scheme: %v", err)
	}
}

func TestOpen_DispatchesByScheme(t *testing.T) {
	snapshotOpeners(t)
	openers = map[string]Opener{}
	var calledA, calledB int32
	var mu sync.Mutex
	RegisterBackend("alpha", func(ctx context.Context, dsn string) (Repo, error) {
		mu.Lock()
		calledA++
		mu.Unlock()
		return nil, nil
	})
	RegisterBackend("beta", func(ctx context.Context, dsn string) (Repo, error) {
		mu.Lock()
		calledB++
		mu.Unlock()
		return nil, nil
	})
	_, _ = Open(context.Background(), "alpha://x")
	_, _ = Open(context.Background(), "beta://y")
	_, _ = Open(context.Background(), "alpha://z")
	if calledA != 2 || calledB != 1 {
		t.Errorf("dispatch count wrong: A=%d B=%d", calledA, calledB)
	}
}

func TestSchemeOf(t *testing.T) {
	cases := map[string]string{
		"sqlite::memory:":                         "sqlite",
		"sqlite:./foo.db":                         "sqlite",
		"sqlite:///abs/path":                      "sqlite",
		"postgres://user:pass@host:5432/db":       "postgres",
		"postgresql://user@host/db?sslmode=disable": "postgres",
		"":                                        "",
		"noscheme":                                "",
	}
	for in, want := range cases {
		if got := schemeOf(in); got != want {
			t.Errorf("schemeOf(%q) = %q, want %q", in, got, want)
		}
	}
}
