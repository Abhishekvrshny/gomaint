package set

// Set represents a collection of unique elements.
type Set struct {
	m map[interface{}]struct{}
}

// NewSet initializes a new set with the given elements.
func NewSet(items ...interface{}) *Set {
	s := &Set{
		m: make(map[interface{}]struct{}),
	}
	s.Add(items...)
	return s
}

// Add inserts elements into the set.
func (s *Set) Add(items ...interface{}) {
	for _, item := range items {
		s.m[item] = struct{}{}
	}
}

// Contains checks if an element exists in the set.
func (s *Set) Contains(item interface{}) bool {
	_, found := s.m[item]
	return found
}

// Remove deletes an element from the set.
func (s *Set) Remove(item interface{}) {
	delete(s.m, item)
}

// Size returns the number of elements in the set.
func (s *Set) Size() int {
	return len(s.m)
}
