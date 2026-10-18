package hlog

import (
	"context"
	"strings"
	"testing"
)

func TestNewTraceIdFormat(t *testing.T) {
	for i := 0; i < 1000; i++ {
		id := NewTraceId()
		if len(id) != 18 {
			t.Fatalf("traceId length = %d (%q), want 18", len(id), id)
		}
		for j := 0; j < len(id); j++ {
			if !strings.ContainsRune(base62Chars, rune(id[j])) {
				t.Fatalf("traceId %q contains invalid base62 char %q", id, id[j])
			}
		}
	}
}

func TestNewTraceIdUnique(t *testing.T) {
	seen := make(map[string]bool, 10000)
	for i := 0; i < 10000; i++ {
		id := NewTraceId()
		if seen[id] {
			t.Fatalf("duplicate traceId: %s", id)
		}
		seen[id] = true
	}
}

func TestWithContextAndFromContext(t *testing.T) {
	id := NewTraceId()
	ctx := WithContext(context.Background(), id)
	if got := FromContext(ctx); got != id {
		t.Fatalf("FromContext = %q, want %q", got, id)
	}
	if got := FromContext(context.Background()); got != "" {
		t.Fatalf("FromContext on plain context = %q, want empty", got)
	}
}

func TestWithContextEmptyTraceId(t *testing.T) {
	ctx := WithContext(context.Background(), "")
	id := FromContext(ctx)
	if len(id) != 18 {
		t.Fatalf("empty traceId must be auto generated, got %q", id)
	}
}

func TestNewContext(t *testing.T) {
	ctx := NewContext()
	if _, ok := ctx.(*Context); !ok {
		t.Fatalf("NewContext must return *Context, got %T", ctx)
	}
	id := FromContext(ctx)
	if len(id) != 18 {
		t.Fatalf("NewContext traceId = %q, want 18 chars", id)
	}
}

func TestContextValue(t *testing.T) {
	ctx := WithContext(context.Background(), "abc")
	val := ctx.Value(contextType)
	c, ok := val.(*Context)
	if !ok {
		t.Fatalf("Value(contextType) = %T, want *Context", val)
	}
	if c.traceId != "abc" {
		t.Fatalf("traceId = %q, want abc", c.traceId)
	}
	// 非 hlog key 走父 context
	if ctx.Value("other") != nil {
		t.Fatal("Value on unknown key must delegate to parent (nil)")
	}
}

func TestContextNested(t *testing.T) {
	ctx1 := WithContext(context.Background(), "id1")
	ctx2 := WithContext(ctx1, "id2")
	if got := FromContext(ctx2); got != "id2" {
		t.Fatalf("inner FromContext = %q, want id2", got)
	}
	if got := FromContext(ctx1); got != "id1" {
		t.Fatalf("outer FromContext = %q, want id1", got)
	}
}
