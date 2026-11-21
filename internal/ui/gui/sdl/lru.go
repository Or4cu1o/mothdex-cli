//go:build gui

package sdl

import "container/list"

// lru is a small least-recently-used cache. The backend keeps the textures of the covers in one, so memory
// stays bounded (docs/contracts/cli/gui.md section 5: at most 48 tiles).
type lru[K comparable, V any] struct {
	max     int
	order   *list.List // front is the most recently used
	items   map[K]*list.Element
	onEvict func(K, V)
}

type lruEntry[K comparable, V any] struct {
	key K
	val V
}

func newLRU[K comparable, V any](max int, onEvict func(K, V)) *lru[K, V] {
	return &lru[K, V]{max: max, order: list.New(), items: map[K]*list.Element{}, onEvict: onEvict}
}

// get returns the value for key and marks it as the most recently used.
func (c *lru[K, V]) get(key K) (V, bool) {
	el, ok := c.items[key]
	if !ok {
		var zero V
		return zero, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*lruEntry[K, V]).val, true //nolint:forcetypeassert // only lruEntry values are stored
}

// put stores a value as the most recently used and evicts the least recently used ones beyond the limit.
func (c *lru[K, V]) put(key K, val V) {
	if el, ok := c.items[key]; ok {
		old := el.Value.(*lruEntry[K, V]) //nolint:forcetypeassert // only lruEntry values are stored
		old.val = val
		c.order.MoveToFront(el)
		return
	}
	c.items[key] = c.order.PushFront(&lruEntry[K, V]{key: key, val: val})
	for c.order.Len() > c.max {
		last := c.order.Back()
		e := c.order.Remove(last).(*lruEntry[K, V]) //nolint:forcetypeassert // only lruEntry values are stored
		delete(c.items, e.key)
		if c.onEvict != nil {
			c.onEvict(e.key, e.val)
		}
	}
}

// clear evicts everything.
func (c *lru[K, V]) clear() {
	for c.order.Len() > 0 {
		e := c.order.Remove(c.order.Back()).(*lruEntry[K, V]) //nolint:forcetypeassert // only lruEntry values are stored
		delete(c.items, e.key)
		if c.onEvict != nil {
			c.onEvict(e.key, e.val)
		}
	}
}

func (c *lru[K, V]) len() int { return c.order.Len() }
