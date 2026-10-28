package ui

import "container/list"

// LRU is a least-recently-used cache bounded by the total size of its values.
// evict is called for every value that leaves the cache so textures can be
// freed. A single value larger than the budget is still kept.
type LRU[K comparable, V any] struct {
	budget, used int
	size         func(V) int
	evict        func(V)
	order        *list.List
	items        map[K]*list.Element
}

type lruEntry[K comparable, V any] struct {
	key  K
	val  V
	size int
}

// NewLRU creates a cache holding up to budget units as measured by size.
func NewLRU[K comparable, V any](budget int, size func(V) int, evict func(V)) *LRU[K, V] {
	return &LRU[K, V]{budget: budget, size: size, evict: evict, order: list.New(), items: map[K]*list.Element{}}
}

// Get returns the value for k and marks it recently used.
func (c *LRU[K, V]) Get(k K) (V, bool) {
	if e, ok := c.items[k]; ok {
		c.order.MoveToFront(e)
		return e.Value.(*lruEntry[K, V]).val, true
	}
	var zero V
	return zero, false
}

// Put stores v under k, evicting the least recently used values while over budget.
func (c *LRU[K, V]) Put(k K, v V) {
	if e, ok := c.items[k]; ok {
		c.remove(e)
	}
	n := c.size(v)
	c.items[k] = c.order.PushFront(&lruEntry[K, V]{key: k, val: v, size: n})
	c.used += n
	for c.used > c.budget && c.order.Len() > 1 {
		c.remove(c.order.Back())
	}
}

func (c *LRU[K, V]) remove(e *list.Element) {
	ent := e.Value.(*lruEntry[K, V])
	c.order.Remove(e)
	delete(c.items, ent.key)
	c.used -= ent.size
	if c.evict != nil {
		c.evict(ent.val)
	}
}

// Clear removes every value, calling evict for each.
func (c *LRU[K, V]) Clear() {
	for c.order.Len() > 0 {
		c.remove(c.order.Back())
	}
}

// Len is the number of cached values.
func (c *LRU[K, V]) Len() int { return c.order.Len() }

// Used is the total size of cached values.
func (c *LRU[K, V]) Used() int { return c.used }
