package ui

import (
	"reflect"
	"testing"
)

func newStringLRU(budget int, evicted *[]string) *LRU[string, string] {
	return NewLRU[string, string](budget,
		func(v string) int { return len(v) },
		func(v string) { *evicted = append(*evicted, v) })
}

func TestLRUEvictsLeastRecentlyUsed(t *testing.T) {
	var evicted []string
	c := newStringLRU(8, &evicted)
	c.Put("a", "aaaa")
	c.Put("b", "bbbb")
	c.Get("a") // a becomes the most recent
	c.Put("c", "cccc")
	if _, ok := c.Get("b"); ok {
		t.Error("b should have been evicted")
	}
	if _, ok := c.Get("a"); !ok {
		t.Error("a should still be cached")
	}
	if !reflect.DeepEqual(evicted, []string{"bbbb"}) {
		t.Errorf("evicted = %v", evicted)
	}
	if c.Used() != 8 {
		t.Errorf("Used = %d, want 8", c.Used())
	}
}

func TestLRUReplaceFreesTheOldValue(t *testing.T) {
	var evicted []string
	c := newStringLRU(8, &evicted)
	c.Put("a", "x")
	c.Put("a", "yy")
	if !reflect.DeepEqual(evicted, []string{"x"}) || c.Used() != 2 || c.Len() != 1 {
		t.Errorf("evicted=%v used=%d len=%d", evicted, c.Used(), c.Len())
	}
}

func TestLRUClearFreesEverything(t *testing.T) {
	var evicted []string
	c := newStringLRU(8, &evicted)
	c.Put("a", "aa")
	c.Put("b", "bb")
	c.Clear()
	if len(evicted) != 2 || c.Len() != 0 || c.Used() != 0 {
		t.Fatalf("evicted=%v used=%d len=%d", evicted, c.Used(), c.Len())
	}
	if _, ok := c.Get("a"); ok {
		t.Fatal("a value outlived Clear")
	}
}

func TestLRUKeepsASingleOversizedValue(t *testing.T) {
	var evicted []string
	c := newStringLRU(2, &evicted)
	c.Put("a", "xxxx")
	if _, ok := c.Get("a"); !ok || len(evicted) != 0 {
		t.Error("the only value should stay even when over budget")
	}
}
