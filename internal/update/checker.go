package update

import (
	"context"
	"sync"
	"time"
)

// Checker resolves the latest release and caches it for an interval. The agent
// publishes the result as the Home Assistant update entity's state, which needs
// a latest version to show that an update exists at all.
//
// The cache is what makes that safe to do on every host: GitHub's
// unauthenticated API allows 60 requests per hour per source address, and a
// fleet behind one NAT address shares that budget. At the default six-hour
// interval a host spends four requests a day.
type Checker struct {
	apiBase  string
	repo     string
	asset    string
	interval time.Duration

	mu     sync.Mutex
	rel    Release
	okAt   time.Time // zero until a lookup has succeeded
	inFlgt bool
}

// NewChecker builds a Checker. A non-positive interval falls back to six hours
// rather than hammering the API on every call.
func NewChecker(apiBase, repo, asset string, interval time.Duration) *Checker {
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	return &Checker{apiBase: apiBase, repo: repo, asset: asset, interval: interval}
}

// Cached returns the last successful lookup without any network access. ok is
// false until one has succeeded, so a caller can publish what it knows
// immediately instead of blocking on GitHub at startup.
func (c *Checker) Cached() (Release, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rel, !c.okAt.IsZero()
}

// Latest returns the cached release, refreshing it first when the cache is
// older than the interval. A failed refresh keeps (and returns) the previous
// value rather than discarding it, so one bad response does not blank the
// entity in Home Assistant.
func (c *Checker) Latest(ctx context.Context) (Release, bool) {
	c.mu.Lock()
	fresh := !c.okAt.IsZero() && time.Since(c.okAt) < c.interval
	c.mu.Unlock()
	if fresh {
		return c.Cached()
	}
	if _, err := c.Refresh(ctx); err != nil {
		return c.Cached()
	}
	return c.Cached()
}

// Refresh resolves the latest release now, ignoring the cache age, and stores
// the result on success. Concurrent callers are serialized; the second one
// re-checks freshness rather than issuing a duplicate request.
func (c *Checker) Refresh(ctx context.Context) (Release, error) {
	c.mu.Lock()
	for c.inFlgt {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return Release{}, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
		c.mu.Lock()
	}
	c.inFlgt = true
	c.mu.Unlock()

	rel, err := Latest(ctx, c.apiBase, c.repo, c.asset)

	c.mu.Lock()
	c.inFlgt = false
	if err == nil {
		c.rel = rel
		c.okAt = time.Now()
	}
	c.mu.Unlock()
	if err != nil {
		return Release{}, err
	}
	return rel, nil
}

// Interval is how often Latest refreshes.
func (c *Checker) Interval() time.Duration { return c.interval }
