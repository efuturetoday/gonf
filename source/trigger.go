package source

import (
	"context"
	"fmt"
	"time"
)

// DefaultPollInterval is how often a document Source checks its content
// while gonf.Watch watches it, unless WithTrigger says otherwise.
const DefaultPollInterval = 2 * time.Second

// Trigger tells a document Source when to check whether its content
// changed: it calls check whenever a check is due and blocks until ctx
// ends. It returns an error only if it can no longer trigger checks.
//
// Every polls; a Trigger driven by file system events can replace it.
type Trigger func(ctx context.Context, check func()) error

// Every returns a Trigger that checks every d. d must be positive.
func Every(d time.Duration) Trigger {
	return func(ctx context.Context, check func()) error {
		if d <= 0 {
			return fmt.Errorf("poll interval %v is not positive", d)
		}
		t := time.NewTicker(d)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-t.C:
				check()
			}
		}
	}
}
