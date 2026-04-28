package knowledge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

const defaultCombinedPromptMaxTokens = 24000

// PromptLoader is the structural subset of core/llmchat.PromptLoader. Keeping
// this interface local avoids an import cycle while still allowing
// knowledge.Loader to plug into llmchat.AgentConfig.PromptLoader.
type PromptLoader interface {
	Load(ctx context.Context) (string, error)
}

// Stats captures token counts from the first successful knowledge-pack load.
type Stats struct {
	APITokens      int
	SiteTokens     int
	CombinedTokens int
}

// Loader wraps the base system-prompt loader and fills both knowledge-pack
// placeholders exactly once. The resolved prompt is cached for the service
// lifetime so per-turn chat handling never performs disk IO.
type Loader struct {
	inner PromptLoader
	api   PromptLoader
	site  PromptLoader
	max   int

	mu     sync.RWMutex
	cached string
	stats  Stats
}

// LoaderOption customizes Loader.
type LoaderOption func(*Loader)

// WithCombinedPromptMaxTokens overrides the hard combined prompt ceiling.
func WithCombinedPromptMaxTokens(max int) LoaderOption {
	return func(l *Loader) {
		if max > 0 {
			l.max = max
		}
	}
}

// NewLoader constructs a lifetime-cached knowledge-pack PromptLoader.
func NewLoader(inner, api, site PromptLoader, opts ...LoaderOption) *Loader {
	l := &Loader{
		inner: inner,
		api:   api,
		site:  site,
		max:   defaultCombinedPromptMaxTokens,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Load returns the cached resolved prompt or builds it on first call.
func (l *Loader) Load(ctx context.Context) (string, error) {
	if l == nil {
		return "", errors.New("llmchat knowledge loader is nil")
	}
	l.mu.RLock()
	if l.cached != "" {
		out := l.cached
		l.mu.RUnlock()
		return out, nil
	}
	l.mu.RUnlock()

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cached != "" {
		return l.cached, nil
	}
	if l.inner == nil || l.api == nil || l.site == nil {
		return "", errors.New("llmchat knowledge loader missing inner/api/site loader")
	}

	base, err := l.inner.Load(ctx)
	if err != nil {
		return "", err
	}
	apiBlob, err := l.api.Load(ctx)
	if err != nil {
		return "", err
	}
	siteBlob, err := l.site.Load(ctx)
	if err != nil {
		return "", err
	}
	out := strings.ReplaceAll(base, APISummaryPlaceholder, apiBlob)
	out = strings.ReplaceAll(out, CordumIOSummaryPlaceholder, siteBlob)

	stats := Stats{
		APITokens:      estimateTokens(apiBlob),
		SiteTokens:     estimateTokens(siteBlob),
		CombinedTokens: estimateTokens(out),
	}
	if stats.CombinedTokens > l.max {
		return "", fmt.Errorf("llmchat knowledge combined system prompt exceeds token budget: tokens=%d max=%d", stats.CombinedTokens, l.max)
	}
	l.cached = out
	l.stats = stats
	return out, nil
}

// Stats returns counts from the first successful Load call.
func (l *Loader) Stats() Stats {
	if l == nil {
		return Stats{}
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.stats
}
