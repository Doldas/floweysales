package instruments

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"floweysales/internal/market"
)

// Cache statuses. A provider that answers "not found" is remembered too, so a
// collection does not ask for the same missing profile on every view.
const (
	StatusOK       = "ok"
	StatusNotFound = "not_found"
)

// How long a cached answer is used before it is asked for again.
const (
	ProfileTTL  = 7 * 24 * time.Hour
	NotFoundTTL = 24 * time.Hour
)

// CachedProfile is a stored answer of one provider for one instrument.
// Provider facts are shared by every user: they say nothing about who asked.
type CachedProfile struct {
	Key
	Status      string    `json:"status"`
	Profile     *Profile  `json:"profile,omitempty"`
	RetrievedAt time.Time `json:"retrieved_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// Fresh reports whether the entry may still be used at now.
func (c CachedProfile) Fresh(now time.Time) bool { return now.Before(c.ExpiresAt) }

// ProfileSource is the part of the market backend that answers the
// "profile" plugin capability.
type ProfileSource interface {
	ProfileProviders() []market.Provider
	Profile(ctx context.Context, p market.Provider, symbol string) (Profile, bool, error)
}

// Store persists the profile cache and each user's overrides. Overrides are
// read and written per user only.
type Store interface {
	GetProfiles(ctx context.Context, keys []Key) (map[Key]CachedProfile, error)
	PutProfile(ctx context.Context, c CachedProfile) error
	// DeleteProfiles clears the cached profiles of one provider, or all with "".
	DeleteProfiles(ctx context.Context, provider string) (int, error)
	GetOverrides(ctx context.Context, userID int64, keys []Key) (map[Key]Override, error)
	// PutOverride stores an override; an empty one deletes it.
	PutOverride(ctx context.Context, userID int64, o Override) error
	DeleteOverride(ctx context.Context, userID int64, k Key) error
}

// MemoryStore is a Store kept in process memory for tests.
type MemoryStore struct {
	mu        sync.Mutex
	profiles  map[Key]CachedProfile
	overrides map[int64]map[Key]Override
	now       func() time.Time
}

// NewMemoryStore returns an empty store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{profiles: map[Key]CachedProfile{}, overrides: map[int64]map[Key]Override{}, now: func() time.Time { return time.Now().UTC() }}
}

// cloneProfile deep-copies through JSON, so callers never share pointers with the store.
func cloneProfile(p *Profile) *Profile {
	if p == nil {
		return nil
	}
	raw, _ := json.Marshal(p)
	var out Profile
	_ = json.Unmarshal(raw, &out)
	return &out
}

func (s *MemoryStore) GetProfiles(_ context.Context, keys []Key) (map[Key]CachedProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[Key]CachedProfile{}
	for _, k := range keys {
		if c, ok := s.profiles[k]; ok {
			c.Profile = cloneProfile(c.Profile)
			out[k] = c
		}
	}
	return out, nil
}

func (s *MemoryStore) PutProfile(_ context.Context, c CachedProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c.Profile = cloneProfile(c.Profile)
	c.RetrievedAt, c.ExpiresAt = c.RetrievedAt.UTC().Truncate(time.Microsecond), c.ExpiresAt.UTC().Truncate(time.Microsecond)
	s.profiles[c.Key] = c
	return nil
}

func (s *MemoryStore) DeleteProfiles(_ context.Context, provider string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for k := range s.profiles {
		if provider == "" || k.Provider == provider {
			delete(s.profiles, k)
			n++
		}
	}
	return n, nil
}

func (s *MemoryStore) GetOverrides(_ context.Context, userID int64, keys []Key) (map[Key]Override, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[Key]Override{}
	for _, k := range keys {
		if o, ok := s.overrides[userID][k]; ok {
			o.Themes = append([]string{}, o.Themes...)
			out[k] = o
		}
	}
	return out, nil
}

func (s *MemoryStore) PutOverride(_ context.Context, userID int64, o Override) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if o.Empty() {
		delete(s.overrides[userID], o.Key)
		return nil
	}
	if s.overrides[userID] == nil {
		s.overrides[userID] = map[Key]Override{}
	}
	o.Themes = append([]string{}, o.Themes...)
	o.UpdatedAt = s.now().Truncate(time.Microsecond)
	s.overrides[userID][o.Key] = o
	return nil
}

func (s *MemoryStore) DeleteOverride(_ context.Context, userID int64, k Key) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.overrides[userID], k)
	return nil
}
