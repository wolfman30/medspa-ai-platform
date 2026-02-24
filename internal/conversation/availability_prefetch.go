package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	moxieclient "github.com/wolfman30/medspa-ai-platform/internal/moxie"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

const prefetchTTL = 5 * time.Minute

// AvailabilityPrefetcher starts background availability lookups as soon as a
// service is identified, before all qualifications are collected. Results are
// cached in Redis and consumed by fetchAndPresentAvailability when ready.
type AvailabilityPrefetcher struct {
	moxieClient *moxieclient.Client
	redis       *redis.Client
	logger      *logging.Logger

	// Track in-flight fetches to avoid duplicate work.
	mu       sync.Mutex
	inflight map[string]bool
}

// NewAvailabilityPrefetcher creates a new prefetcher.
func NewAvailabilityPrefetcher(
	moxieClient *moxieclient.Client,
	rdb *redis.Client,
	logger *logging.Logger,
) *AvailabilityPrefetcher {
	return &AvailabilityPrefetcher{
		moxieClient: moxieClient,
		redis:       rdb,
		logger:      logger,
		inflight:    make(map[string]bool),
	}
}

// prefetchCacheKey returns the Redis key for cached availability results.
func prefetchCacheKey(orgID, service string) string {
	return fmt.Sprintf("avail_prefetch:%s:%s", orgID, strings.ToLower(service))
}

// CachedAvailability holds pre-fetched availability results.
type CachedAvailability struct {
	Result    *AvailabilityResult `json:"result"`
	FetchedAt time.Time           `json:"fetched_at"`
	Service   string              `json:"service"`
}

// StartPrefetch kicks off a background availability fetch for the given service.
// Safe to call multiple times — duplicates are deduplicated.
func (p *AvailabilityPrefetcher) StartPrefetch(
	ctx context.Context,
	orgID string,
	cfg *clinic.Config,
	serviceInterest string,
	providerPreference string,
) {
	if cfg == nil || !cfg.UsesMoxieBooking() || cfg.BookingURL == "" {
		return
	}
	if p.moxieClient == nil {
		return
	}
	if p.redis == nil {
		return
	}

	resolvedService := cfg.ResolveServiceName(serviceInterest)
	cacheKey := prefetchCacheKey(orgID, resolvedService)

	// Deduplicate in-flight fetches.
	p.mu.Lock()
	if p.inflight[cacheKey] {
		p.mu.Unlock()
		return
	}
	p.inflight[cacheKey] = true
	p.mu.Unlock()

	p.logger.Info("availability prefetch: starting",
		"org_id", orgID,
		"service", serviceInterest,
		"resolved", resolvedService,
	)

	go func() {
		defer func() {
			p.mu.Lock()
			delete(p.inflight, cacheKey)
			p.mu.Unlock()
		}()

		fetchCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		timePrefs := TimePreferences{} // Broad fetch — no time filters yet.
		var result *AvailabilityResult
		var err error

		if p.moxieClient != nil && cfg.MoxieConfig != nil {
			result, err = FetchAvailableTimesFromMoxieAPIWithProvider(
				fetchCtx, p.moxieClient, cfg, resolvedService,
				providerPreference, timePrefs, nil, serviceInterest,
			)
		}

		if err != nil {
			p.logger.Warn("availability prefetch: failed", "error", err, "service", resolvedService)
			return
		}
		if result == nil || len(result.Slots) == 0 {
			p.logger.Info("availability prefetch: no slots", "service", resolvedService)
			return
		}

		cached := &CachedAvailability{
			Result:    result,
			FetchedAt: time.Now(),
			Service:   serviceInterest,
		}
		data, _ := json.Marshal(cached)
		if err := p.redis.Set(fetchCtx, cacheKey, string(data), prefetchTTL).Err(); err != nil {
			p.logger.Warn("availability prefetch: cache write failed", "error", err)
			return
		}

		p.logger.Info("availability prefetch: cached",
			"service", resolvedService,
			"slots", len(result.Slots),
		)
	}()
}

// GetCached returns cached availability if available and fresh. Returns nil if miss.
func (p *AvailabilityPrefetcher) GetCached(
	ctx context.Context,
	orgID string,
	service string,
	cfg *clinic.Config,
) *CachedAvailability {
	if p.redis == nil {
		return nil
	}

	resolvedService := service
	if cfg != nil {
		resolvedService = cfg.ResolveServiceName(service)
	}
	cacheKey := prefetchCacheKey(orgID, resolvedService)

	data, err := p.redis.Get(ctx, cacheKey).Result()
	if err != nil || data == "" {
		return nil
	}

	var cached CachedAvailability
	if err := json.Unmarshal([]byte(data), &cached); err != nil {
		return nil
	}

	if time.Since(cached.FetchedAt) > prefetchTTL {
		return nil
	}

	return &cached
}
