package weavert

import (
	"os"
	"os/exec"
	"testing"
)

func TestToString(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want string
	}{
		{"nil", nil, "nil"},
		{"string", "hi", "hi"},
		{"true", true, "true"},
		{"false", false, "false"},
		{"whole number", 5.0, "5"},
		{"fractional number", 1.5, "1.5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ToString(tt.v); got != tt.want {
				t.Errorf("ToString(%v) = %q, want %q", tt.v, got, tt.want)
			}
		})
	}
}

func TestLen_Object(t *testing.T) {
	o := NewObject()
	ObjSet(o, "x", 1.0)
	ObjSet(o, "y", 2.0)
	if got := Len(o); got != 2.0 {
		t.Errorf("Len(object) = %v, want 2", got)
	}
}

func TestLen_String(t *testing.T) {
	if got := Len("hello"); got != 5.0 {
		t.Errorf("Len(hello) = %v, want 5", got)
	}
}

func TestLen_StringCountsRunesNotBytes(t *testing.T) {
	if got := Len("日本語"); got != 3.0 {
		t.Errorf("Len(日本語) = %v, want 3 (rune count, not byte count)", got)
	}
}

func TestLen_InvalidTypePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected Len(5.0) to panic")
		}
	}()
	Len(5.0)
}

func TestArgs_MatchesOSArgsIncludingProgramName(t *testing.T) {
	got := Args().(Object)
	if len(got) != len(os.Args) {
		t.Fatalf("Args() has %d elements, want %d (len(os.Args))", len(got), len(os.Args))
	}
	for i, want := range os.Args {
		if v := ObjAt(got, float64(i)); v != want {
			t.Errorf("Args()[%d] = %v, want %q", i, v, want)
		}
	}
}

// TestExit_TerminatesProcessWithGivenCode can't call Exit directly — it
// would kill the test binary itself. Instead it re-invokes this same
// test binary as a subprocess (the standard Go idiom for testing an
// os.Exit call), asking only that one subprocess to actually call Exit,
// and checks the *parent* process observes the expected exit code.
func TestExit_TerminatesProcessWithGivenCode(t *testing.T) {
	if os.Getenv("WEAVERT_EXIT_TEST_SUBPROCESS") == "1" {
		Exit(7.0)
		t.Fatal("Exit(7) returned instead of terminating the process")
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestExit_TerminatesProcessWithGivenCode")
	cmd.Env = append(os.Environ(), "WEAVERT_EXIT_TEST_SUBPROCESS=1")
	err := cmd.Run()
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("subprocess ended with %v (%T), want a non-zero *exec.ExitError", err, err)
	}
	if got := exitErr.ExitCode(); got != 7 {
		t.Errorf("subprocess exit code = %d, want 7", got)
	}
}

func TestExit_NonNumberPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal(`expected Exit("x") to panic`)
		}
	}()
	Exit("x")
}
