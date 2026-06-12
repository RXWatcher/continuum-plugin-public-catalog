package store

import (
	"reflect"
	"testing"
)

func TestAllowedLibraryIDsDefaultDeny(t *testing.T) {
	s := &Store{}

	// No allowlist configured: deny everything, regardless of request.
	if ids, deny := s.allowedLibraryIDs(nil); !deny || ids != nil {
		t.Fatalf("empty allowlist must deny all; got ids=%v deny=%v", ids, deny)
	}
	if _, deny := s.allowedLibraryIDs([]string{"1", "2"}); !deny {
		t.Fatalf("empty allowlist must deny even when libraries requested")
	}
}

func TestAllowedLibraryIDsIntersection(t *testing.T) {
	s := &Store{}
	s.SetPublicLibraryIDs([]string{"1", "2", "3"})

	// Empty request -> full allowlist.
	ids, deny := s.allowedLibraryIDs(nil)
	if deny {
		t.Fatalf("non-empty allowlist with empty request must not deny")
	}
	if !reflect.DeepEqual(ids, []int{1, 2, 3}) {
		t.Fatalf("empty request should yield full allowlist, got %v", ids)
	}

	// Request narrows within the allowlist.
	ids, deny = s.allowedLibraryIDs([]string{"2", "3"})
	if deny || !reflect.DeepEqual(ids, []int{2, 3}) {
		t.Fatalf("request within allowlist should narrow, got ids=%v deny=%v", ids, deny)
	}

	// Request for a forbidden library is dropped; mix keeps only allowed.
	ids, deny = s.allowedLibraryIDs([]string{"3", "9"})
	if deny || !reflect.DeepEqual(ids, []int{3}) {
		t.Fatalf("forbidden id must be dropped, got ids=%v deny=%v", ids, deny)
	}

	// Request for ONLY forbidden libraries -> denyAll (can't widen).
	if _, deny := s.allowedLibraryIDs([]string{"9", "10"}); !deny {
		t.Fatalf("request for only-forbidden libraries must deny all")
	}
}

func TestScopePublicLibraryIDsStrings(t *testing.T) {
	s := &Store{}
	s.SetPublicLibraryIDs([]string{"5", "7"})

	ids, deny := s.ScopePublicLibraryIDs([]string{"7"})
	if deny || !reflect.DeepEqual(ids, []string{"7"}) {
		t.Fatalf("expected [7], got ids=%v deny=%v", ids, deny)
	}
	if _, deny := s.ScopePublicLibraryIDs([]string{"99"}); !deny {
		t.Fatalf("forbidden-only request must deny")
	}

	empty := &Store{}
	if _, deny := empty.ScopePublicLibraryIDs(nil); !deny {
		t.Fatalf("no allowlist must deny all")
	}
}
