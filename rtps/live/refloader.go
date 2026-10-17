package live

import (
	"context"
	"sync"
	"time"

	"github.com/arush-sal/garage/backend/rtps/processing"
)

// RefLoader keeps the DeltaTracker's stored reference lap in step with the live track. It wraps the
// detector's TrackEnricher (forwarding every call unchanged), so it sees each lock and clear: a
// CONFIDENT track loads that track's stored reference lap (pinned, else fastest; see
// processing.ResolveTrackReference) on its own worker goroutine, never on the ingest goroutine that
// calls SetTrack; anything else (provisional, cleared) drops the reference at once. Reload re-reads it
// after the user pins or unpins a lap on the current track, and LapStored after a lap is stored that
// could now be the reference (the track's first stored lap, or a new fastest one while unpinned).
//
// gen fences a slow load: a result is installed only if no track change or reload happened since it
// started, so a stale lap can never land after the track moved on.
type RefLoader struct {
	inner   processing.TrackEnricher
	delta   *DeltaTracker
	resolve func(ctx context.Context, slug string) (LoadedRef, error)
	onErr   func(error)

	mu     sync.Mutex
	slug   string    // current confident track ("" = none)
	gen    uint64    // bumped on every track change and reload
	loaded LoadedRef // what is installed in delta for slug (zero = nothing)

	retryBase time.Duration // first retry delay after a failed load (tests shorten it)

	notify chan struct{} // 1-deep wakeup for the worker
	stop   chan struct{}
	wg     sync.WaitGroup
	once   sync.Once
}

// LoadedRef is a resolved stored reference lap: its normalized trace (nil = the track has none),
// whether it is the user's pin, and its lap time - what LapStored needs to tell whether a newly
// stored lap could replace it.
type LoadedRef struct {
	Lap       *processing.NormalizedLap
	Pinned    bool
	LapTime   time.Duration
	SessionID string // the session holding the lap (its removal invalidates the reference)
}

// Retry backoff for a failed load: retryBase, doubling, capped at retryMax, while the request is
// current. Failures are storage errors (a database blip); the reference is not worth more than this.
const (
	defaultRetryBase = time.Second
	retryMax         = 30 * time.Second
)

// NewRefLoader starts the worker. resolve returns a slug's stored reference (Lap nil when it has
// none). onErr (nil = ignore) receives resolve failures.
func NewRefLoader(inner processing.TrackEnricher, delta *DeltaTracker, resolve func(ctx context.Context, slug string) (LoadedRef, error), onErr func(error)) *RefLoader {
	if onErr == nil {
		onErr = func(error) {}
	}
	l := &RefLoader{inner: inner, delta: delta, resolve: resolve, onErr: onErr, retryBase: defaultRetryBase,
		notify: make(chan struct{}, 1), stop: make(chan struct{})}
	l.wg.Add(1)
	go l.worker()
	return l
}

// SetTrack forwards to the wrapped enricher, then follows the track: load on confident, drop otherwise.
func (l *RefLoader) SetTrack(sessionID string, slug *string, confidence *string) {
	l.inner.SetTrack(sessionID, slug, confidence)
	want := ""
	if slug != nil && confidence != nil && *confidence == processing.ConfidenceConfident {
		want = *slug
	}
	l.follow(want)
}

// ClearTrack forwards to the wrapped enricher and drops the stored reference.
func (l *RefLoader) ClearTrack(sessionID string) {
	l.inner.ClearTrack(sessionID)
	l.follow("")
}

// Reload re-reads slug's stored reference if slug is the current confident track (a pin change).
func (l *RefLoader) Reload(slug string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if slug == "" || slug != l.slug {
		return
	}
	l.gen++
	l.wake()
}

// LapStored is told about every stored lap (LapSegmenter.OnSaved). It reloads the reference only when
// the lap could change it: an eligible lap (processing.RefEligible) on the current confident track,
// when nothing is loaded yet or the loaded reference is unpinned and slower. Once per lap, never per
// frame; the load runs on the worker.
func (l *RefLoader) LapStored(lap processing.Lap) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.slug == "" || !processing.RefEligible(&lap, l.slug) {
		return
	}
	if l.loaded.Lap != nil && (l.loaded.Pinned || lap.Duration >= l.loaded.LapTime) {
		return
	}
	l.gen++
	l.wake()
}

// SessionRemoved is told when a session's laps are deleted (a discarded session). If the loaded
// reference came from it - its pin, if any, is gone with it (FK cascade) - the reference is reloaded,
// falling back to the track's fastest remaining lap.
func (l *RefLoader) SessionRemoved(sessionID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.slug == "" || l.loaded.Lap == nil || l.loaded.SessionID != sessionID {
		return
	}
	l.gen++
	l.wake()
}

// follow switches to slug ("" = none): the old reference is dropped immediately, a new one queued.
func (l *RefLoader) follow(slug string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if slug == l.slug {
		return
	}
	l.slug = slug
	l.gen++
	l.loaded = LoadedRef{}
	l.delta.SetStoredReference(nil)
	if slug != "" {
		l.wake()
	}
}

// wake signals the worker without blocking. Caller holds l.mu.
func (l *RefLoader) wake() {
	select {
	case l.notify <- struct{}{}:
	default:
	}
}

func (l *RefLoader) worker() {
	defer l.wg.Done()
	for {
		select {
		case <-l.stop:
			return
		case <-l.notify:
		}
		l.mu.Lock()
		slug, gen := l.slug, l.gen
		l.mu.Unlock()
		if slug != "" {
			l.load(slug, gen)
		}
	}
}

// load resolves slug's reference and installs it if gen is still current. A failure is retried with
// bounded backoff while gen stays current; a newer request (a wakeup) ends it and is re-signalled for
// the worker loop.
func (l *RefLoader) load(slug string, gen uint64) {
	delay := l.retryBase
	for {
		ref, err := l.resolve(context.Background(), slug)
		if err == nil {
			l.mu.Lock()
			if gen == l.gen {
				l.loaded = ref
				l.delta.SetStoredReference(ref.Lap)
			}
			l.mu.Unlock()
			return
		}
		l.onErr(err)
		select {
		case <-l.stop:
			return
		case <-l.notify: // a newer request supersedes this one: hand it back to the loop
			l.mu.Lock()
			l.wake()
			l.mu.Unlock()
			return
		case <-time.After(delay):
		}
		l.mu.Lock()
		current := gen == l.gen
		l.mu.Unlock()
		if !current {
			return
		}
		delay = min(2*delay, retryMax)
	}
}

// Close stops the worker. Idempotent.
func (l *RefLoader) Close() {
	l.once.Do(func() { close(l.stop) })
	l.wg.Wait()
}
