package queue

import (
	"context"
	"time"
)

type heldObjectsKey struct{}

// WithObject drains the previous handler before a user mutation publishes a
// replacement generation. Interrupting it does not spend a retry, and its
// operation remains runnable if the new business transaction rolls back.
func (c *Client) WithObject(ctx context.Context, id string, mutation func(context.Context) error) error {
	held, _ := ctx.Value(heldObjectsKey{}).(map[string]bool)
	if id == "" || held[id] {
		return mutation(ctx)
	}
	for {
		c.mu.Lock()
		owner := c.objects[id]
		current, _ := GetTaskID(ctx)
		if owner == "" || (current != "" && owner == current) {
			reserved := owner == ""
			if reserved {
				c.objects[id] = "mutation"
			}
			c.mu.Unlock()
			if reserved {
				defer func() { c.mu.Lock(); delete(c.objects, id); c.mu.Unlock() }()
			}
			next := make(map[string]bool, len(held)+1)
			for key, value := range held {
				next[key] = value
			}
			next[id] = true
			return mutation(context.WithValue(ctx, heldObjectsKey{}, next))
		}
		if owner != "mutation" {
			c.suspending[owner] = true
			if stop := c.active[owner]; stop != nil {
				stop()
			}
		}
		c.mu.Unlock()
		if !waitContext(ctx, 10*time.Millisecond) {
			return ctx.Err()
		}
	}
}
