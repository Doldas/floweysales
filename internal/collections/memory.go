package collections

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemoryStore is a Store kept in process memory. It backs tests and follows
// the same rules as the MySQL store, which the shared conformance test checks.
type MemoryStore struct {
	mu          sync.Mutex
	next        int64
	collections map[int64]*memCollection
	now         func() time.Time
}

type memCollection struct {
	owner   int64
	c       Collection
	members []Member
}

// NewMemoryStore returns an empty store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{collections: map[int64]*memCollection{}, now: func() time.Time { return time.Now().UTC() }}
}

func (s *MemoryStore) id() int64 { s.next++; return s.next }

func (s *MemoryStore) owned(userID, id int64) (*memCollection, error) {
	mc, ok := s.collections[id]
	if !ok || mc.owner != userID {
		return nil, ErrNotFound
	}
	return mc, nil
}

func (mc *memCollection) view() Collection {
	c := mc.c
	c.Tags = append([]string{}, c.Tags...)
	c.MemberCount = len(mc.members)
	return c
}

func copyMember(m Member) Member {
	m.Tags = append([]string{}, m.Tags...)
	if m.Weight != nil {
		w := *m.Weight
		m.Weight = &w
	}
	return m
}

func (s *MemoryStore) nameTaken(userID int64, name string, exceptID int64) bool {
	for id, mc := range s.collections {
		if mc.owner == userID && id != exceptID && strings.EqualFold(mc.c.Name, name) {
			return true
		}
	}
	return false
}

func (s *MemoryStore) ListCollections(_ context.Context, userID int64) ([]Collection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Collection{}
	for _, mc := range s.collections {
		if mc.owner == userID {
			out = append(out, mc.view())
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *MemoryStore) GetCollection(_ context.Context, userID, id int64) (Collection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mc, err := s.owned(userID, id)
	if err != nil {
		return Collection{}, err
	}
	return mc.view(), nil
}

func (s *MemoryStore) CreateCollection(_ context.Context, userID int64, c Collection) (Collection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, mc := range s.collections {
		if mc.owner == userID {
			count++
		}
	}
	if count >= MaxCollectionsPerUser {
		return Collection{}, ErrLimit
	}
	if s.nameTaken(userID, c.Name, 0) {
		return Collection{}, ErrNameTaken
	}
	c.ID = s.id()
	c.CreatedAt, c.UpdatedAt = s.now(), s.now()
	c.Tags = append([]string{}, c.Tags...)
	s.collections[c.ID] = &memCollection{owner: userID, c: c}
	return s.collections[c.ID].view(), nil
}

func (s *MemoryStore) UpdateCollection(_ context.Context, userID int64, c Collection) (Collection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mc, err := s.owned(userID, c.ID)
	if err != nil {
		return Collection{}, err
	}
	if s.nameTaken(userID, c.Name, c.ID) {
		return Collection{}, ErrNameTaken
	}
	mc.c.Name, mc.c.Description, mc.c.Category, mc.c.Archived = c.Name, c.Description, c.Category, c.Archived
	mc.c.Tags = append([]string{}, c.Tags...)
	mc.c.UpdatedAt = s.now()
	return mc.view(), nil
}

func (s *MemoryStore) DeleteCollection(_ context.Context, userID, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.owned(userID, id); err != nil {
		return err
	}
	delete(s.collections, id)
	return nil
}

func (s *MemoryStore) ListMembers(_ context.Context, userID, collectionID int64, page Page) ([]Member, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mc, err := s.owned(userID, collectionID)
	if err != nil {
		return nil, 0, err
	}
	page = page.Normalize()
	out := []Member{}
	for i := page.Offset; i < len(mc.members) && len(out) < page.Limit; i++ {
		out = append(out, copyMember(mc.members[i]))
	}
	return out, len(mc.members), nil
}

func (s *MemoryStore) AllMembers(ctx context.Context, userID, collectionID int64) ([]Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mc, err := s.owned(userID, collectionID)
	if err != nil {
		return nil, err
	}
	out := make([]Member, 0, len(mc.members))
	for _, m := range mc.members {
		out = append(out, copyMember(m))
	}
	return out, nil
}

func (s *MemoryStore) addLocked(mc *memCollection, m Member) (Member, error) {
	if len(mc.members) >= MaxMembers {
		return Member{}, ErrLimit
	}
	for _, x := range mc.members {
		if x.Instrument.Key() == m.Instrument.Key() {
			return Member{}, ErrDuplicateMember
		}
	}
	m.ID = s.id()
	m.CollectionID = mc.c.ID
	m.AddedAt = s.now()
	m = copyMember(m)
	mc.members = append(mc.members, m)
	mc.c.UpdatedAt = s.now()
	return copyMember(m), nil
}

func (s *MemoryStore) AddMember(_ context.Context, userID, collectionID int64, m Member) (Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mc, err := s.owned(userID, collectionID)
	if err != nil {
		return Member{}, err
	}
	return s.addLocked(mc, m)
}

func (s *MemoryStore) UpdateMember(_ context.Context, userID int64, m Member) (Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mc, err := s.owned(userID, m.CollectionID)
	if err != nil {
		return Member{}, err
	}
	for i, x := range mc.members {
		if x.ID == m.ID {
			x.Note, x.Tags, x.Weight = m.Note, m.Tags, m.Weight
			mc.members[i] = copyMember(x)
			mc.c.UpdatedAt = s.now()
			return copyMember(x), nil
		}
	}
	return Member{}, ErrMemberNotFound
}

func (s *MemoryStore) RemoveMember(_ context.Context, userID, collectionID, memberID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	mc, err := s.owned(userID, collectionID)
	if err != nil {
		return err
	}
	for i, x := range mc.members {
		if x.ID == memberID {
			mc.members = append(mc.members[:i], mc.members[i+1:]...)
			mc.c.UpdatedAt = s.now()
			return nil
		}
	}
	return ErrMemberNotFound
}

func (s *MemoryStore) TransferMember(_ context.Context, userID, fromID, memberID, toID int64, move bool, weight *float64) (Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	from, err := s.owned(userID, fromID)
	if err != nil {
		return Member{}, err
	}
	to, err := s.owned(userID, toID)
	if err != nil {
		return Member{}, err
	}
	idx := -1
	for i, x := range from.members {
		if x.ID == memberID {
			idx = i
		}
	}
	if idx < 0 {
		return Member{}, ErrMemberNotFound
	}
	m := copyMember(from.members[idx])
	m.Weight = weight
	added, err := s.addLocked(to, m)
	if err != nil {
		return Member{}, err
	}
	if move {
		from.members = append(from.members[:idx], from.members[idx+1:]...)
		from.c.UpdatedAt = s.now()
	}
	return added, nil
}

func (s *MemoryStore) CollectionsContaining(_ context.Context, userID int64, provider, symbol string) ([]Collection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := Instrument{Provider: provider, Symbol: symbol}.Key()
	out := []Collection{}
	for _, mc := range s.collections {
		if mc.owner != userID {
			continue
		}
		for _, m := range mc.members {
			if m.Instrument.Key() == key {
				out = append(out, mc.view())
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
