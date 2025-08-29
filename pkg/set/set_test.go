package set

import (
	"testing"
)

func TestNewSet(t *testing.T) {
	t.Run("empty set", func(t *testing.T) {
		s := NewSet()
		if s.Size() != 0 {
			t.Errorf("expected empty set size 0, got %d", s.Size())
		}
	})

	t.Run("set with initial elements", func(t *testing.T) {
		s := NewSet(1, 2, 3)
		if s.Size() != 3 {
			t.Errorf("expected set size 3, got %d", s.Size())
		}
		if !s.Contains(1) || !s.Contains(2) || !s.Contains(3) {
			t.Error("set should contain initial elements 1, 2, 3")
		}
	})

	t.Run("set with duplicate initial elements", func(t *testing.T) {
		s := NewSet(1, 1, 2, 2, 3)
		if s.Size() != 3 {
			t.Errorf("expected set size 3 with duplicates removed, got %d", s.Size())
		}
		if !s.Contains(1) || !s.Contains(2) || !s.Contains(3) {
			t.Error("set should contain unique elements 1, 2, 3")
		}
	})

	t.Run("set with mixed types", func(t *testing.T) {
		s := NewSet(1, "hello", 3.14, true)
		if s.Size() != 4 {
			t.Errorf("expected set size 4, got %d", s.Size())
		}
		if !s.Contains(1) || !s.Contains("hello") || !s.Contains(3.14) || !s.Contains(true) {
			t.Error("set should contain all mixed type elements")
		}
	})
}

func TestAdd(t *testing.T) {
	t.Run("add single element", func(t *testing.T) {
		s := NewSet()
		s.Add(42)
		if s.Size() != 1 {
			t.Errorf("expected set size 1, got %d", s.Size())
		}
		if !s.Contains(42) {
			t.Error("set should contain added element 42")
		}
	})

	t.Run("add multiple elements", func(t *testing.T) {
		s := NewSet()
		s.Add(1, 2, 3, 4, 5)
		if s.Size() != 5 {
			t.Errorf("expected set size 5, got %d", s.Size())
		}
		for i := 1; i <= 5; i++ {
			if !s.Contains(i) {
				t.Errorf("set should contain element %d", i)
			}
		}
	})

	t.Run("add duplicate elements", func(t *testing.T) {
		s := NewSet()
		s.Add(1)
		s.Add(1) // Add duplicate
		if s.Size() != 1 {
			t.Errorf("expected set size 1 after adding duplicate, got %d", s.Size())
		}
		if !s.Contains(1) {
			t.Error("set should contain element 1")
		}
	})

	t.Run("add to existing set", func(t *testing.T) {
		s := NewSet(1, 2, 3)
		s.Add(4, 5)
		if s.Size() != 5 {
			t.Errorf("expected set size 5, got %d", s.Size())
		}
		for i := 1; i <= 5; i++ {
			if !s.Contains(i) {
				t.Errorf("set should contain element %d", i)
			}
		}
	})

	t.Run("add nil element", func(t *testing.T) {
		s := NewSet()
		s.Add(nil)
		if s.Size() != 1 {
			t.Errorf("expected set size 1, got %d", s.Size())
		}
		if !s.Contains(nil) {
			t.Error("set should contain nil element")
		}
	})
}

func TestContains(t *testing.T) {
	t.Run("contains existing element", func(t *testing.T) {
		s := NewSet(1, 2, 3)
		if !s.Contains(2) {
			t.Error("set should contain element 2")
		}
	})

	t.Run("does not contain non-existing element", func(t *testing.T) {
		s := NewSet(1, 2, 3)
		if s.Contains(4) {
			t.Error("set should not contain element 4")
		}
	})

	t.Run("contains nil", func(t *testing.T) {
		s := NewSet(nil)
		if !s.Contains(nil) {
			t.Error("set should contain nil element")
		}
	})

	t.Run("empty set contains nothing", func(t *testing.T) {
		s := NewSet()
		if s.Contains(1) {
			t.Error("empty set should not contain any element")
		}
	})

	t.Run("contains different types", func(t *testing.T) {
		s := NewSet("hello", 42, 3.14, true)
		if !s.Contains("hello") {
			t.Error("set should contain string 'hello'")
		}
		if !s.Contains(42) {
			t.Error("set should contain int 42")
		}
		if !s.Contains(3.14) {
			t.Error("set should contain float 3.14")
		}
		if !s.Contains(true) {
			t.Error("set should contain bool true")
		}
		if s.Contains(false) {
			t.Error("set should not contain bool false")
		}
	})
}

func TestRemove(t *testing.T) {
	t.Run("remove existing element", func(t *testing.T) {
		s := NewSet(1, 2, 3)
		s.Remove(2)
		if s.Size() != 2 {
			t.Errorf("expected set size 2 after removal, got %d", s.Size())
		}
		if s.Contains(2) {
			t.Error("set should not contain removed element 2")
		}
		if !s.Contains(1) || !s.Contains(3) {
			t.Error("set should still contain elements 1 and 3")
		}
	})

	t.Run("remove non-existing element", func(t *testing.T) {
		s := NewSet(1, 2, 3)
		originalSize := s.Size()
		s.Remove(4) // Element doesn't exist
		if s.Size() != originalSize {
			t.Errorf("set size should remain %d after removing non-existing element, got %d", originalSize, s.Size())
		}
	})

	t.Run("remove from empty set", func(t *testing.T) {
		s := NewSet()
		s.Remove(1) // Should not panic
		if s.Size() != 0 {
			t.Errorf("empty set size should remain 0, got %d", s.Size())
		}
	})

	t.Run("remove nil element", func(t *testing.T) {
		s := NewSet(nil, 1, 2)
		s.Remove(nil)
		if s.Size() != 2 {
			t.Errorf("expected set size 2 after removing nil, got %d", s.Size())
		}
		if s.Contains(nil) {
			t.Error("set should not contain nil after removal")
		}
	})

	t.Run("remove all elements", func(t *testing.T) {
		s := NewSet(1, 2, 3)
		s.Remove(1)
		s.Remove(2)
		s.Remove(3)
		if s.Size() != 0 {
			t.Errorf("expected empty set after removing all elements, got size %d", s.Size())
		}
	})
}

func TestSize(t *testing.T) {
	t.Run("empty set size", func(t *testing.T) {
		s := NewSet()
		if s.Size() != 0 {
			t.Errorf("expected empty set size 0, got %d", s.Size())
		}
	})

	t.Run("single element size", func(t *testing.T) {
		s := NewSet(42)
		if s.Size() != 1 {
			t.Errorf("expected set size 1, got %d", s.Size())
		}
	})

	t.Run("multiple elements size", func(t *testing.T) {
		s := NewSet(1, 2, 3, 4, 5)
		if s.Size() != 5 {
			t.Errorf("expected set size 5, got %d", s.Size())
		}
	})

	t.Run("size after operations", func(t *testing.T) {
		s := NewSet()

		// Add elements
		s.Add(1, 2, 3)
		if s.Size() != 3 {
			t.Errorf("expected set size 3 after adding, got %d", s.Size())
		}

		// Remove element
		s.Remove(2)
		if s.Size() != 2 {
			t.Errorf("expected set size 2 after removing, got %d", s.Size())
		}

		// Add duplicate
		s.Add(1)
		if s.Size() != 2 {
			t.Errorf("expected set size 2 after adding duplicate, got %d", s.Size())
		}
	})
}

func TestSetOperations(t *testing.T) {
	t.Run("complex operations sequence", func(t *testing.T) {
		s := NewSet()

		// Start empty
		if s.Size() != 0 {
			t.Error("set should start empty")
		}

		// Add elements
		s.Add("a", "b", "c")
		if s.Size() != 3 || !s.Contains("a") || !s.Contains("b") || !s.Contains("c") {
			t.Error("set should contain a, b, c")
		}

		// Add duplicates
		s.Add("a", "b")
		if s.Size() != 3 {
			t.Error("set size should remain 3 after adding duplicates")
		}

		// Remove element
		s.Remove("b")
		if s.Size() != 2 || s.Contains("b") {
			t.Error("set should not contain b after removal")
		}

		// Add new element
		s.Add("d")
		if s.Size() != 3 || !s.Contains("d") {
			t.Error("set should contain d after addition")
		}

		// Final state check
		expected := []interface{}{"a", "c", "d"}
		for _, item := range expected {
			if !s.Contains(item) {
				t.Errorf("set should contain %v", item)
			}
		}
	})
}
