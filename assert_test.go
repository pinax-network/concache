package concache

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// The package has no dependencies, tests included, so the helpers below stand in for the handful of
// assertions the tests need.
//
// assert* reports the failure and lets the test continue; require* stops it. The distinction
// matters: the concurrency tests assert from goroutines, where t.Fatal is not allowed, so anything
// called from a goroutine has to be an assert.
//
// Arguments are (got, want), matching the "got X, want Y" convention of the standard library rather
// than the (expected, actual) order of assertion libraries.

func assertEqual[T comparable](t *testing.T, got, want T, msg ...string) {

	t.Helper()

	if got != want {
		t.Errorf("got %v, want %v%s", got, want, note(msg))
	}
}

func requireEqual[T comparable](t *testing.T, got, want T, msg ...string) {

	t.Helper()

	if got != want {
		t.Fatalf("got %v, want %v%s", got, want, note(msg))
	}
}

func assertNotEqual[T comparable](t *testing.T, got, unwanted T, msg ...string) {

	t.Helper()

	if got == unwanted {
		t.Errorf("got %v, want anything else%s", got, note(msg))
	}
}

// assertTimeEqual compares instants through time.Time.Equal instead of ==, which would also compare
// the monotonic reading and the location.
func assertTimeEqual(t *testing.T, got, want time.Time, msg ...string) {

	t.Helper()

	if !got.Equal(want) {
		t.Errorf("got %v, want %v%s", got, want, note(msg))
	}
}

func assertTrue(t *testing.T, got bool, msg ...string) {

	t.Helper()

	if !got {
		t.Errorf("got false, want true%s", note(msg))
	}
}

func assertFalse(t *testing.T, got bool, msg ...string) {

	t.Helper()

	if got {
		t.Errorf("got true, want false%s", note(msg))
	}
}

func assertNoError(t *testing.T, err error, msg ...string) {

	t.Helper()

	if err != nil {
		t.Errorf("unexpected error: %v%s", err, note(msg))
	}
}

func requireNoError(t *testing.T, err error, msg ...string) {

	t.Helper()

	if err != nil {
		t.Fatalf("unexpected error: %v%s", err, note(msg))
	}
}

func assertErrorIs(t *testing.T, err, target error, msg ...string) {

	t.Helper()

	if !errors.Is(err, target) {
		t.Errorf("got error %v, want %v%s", err, target, note(msg))
	}
}

// note renders the optional explanation of an assertion.
func note(msg []string) string {

	if len(msg) == 0 {
		return ""
	}

	return " (" + strings.Join(msg, " ") + ")"
}
