package mutagen_test

import (
	"testing"

	"github.com/plombardi89/codebox/internal/mutagen"
)

func TestSessionName_Deterministic(t *testing.T) {
	a := mutagen.SessionName("mybox", "./src", "~/src")
	b := mutagen.SessionName("mybox", "./src", "~/src")

	if a != b {
		t.Errorf("same inputs should produce same name: %q != %q", a, b)
	}
}

func TestSessionName_DifferentInputs(t *testing.T) {
	a := mutagen.SessionName("mybox", "./src", "~/src")
	b := mutagen.SessionName("mybox", "./config", "~/config")

	if a == b {
		t.Errorf("different inputs should produce different names: both %q", a)
	}
}

func TestSessionName_DifferentBoxes(t *testing.T) {
	a := mutagen.SessionName("box1", "./src", "~/src")
	b := mutagen.SessionName("box2", "./src", "~/src")

	if a == b {
		t.Errorf("different box names should produce different names: both %q", a)
	}
}

func TestSessionName_Format(t *testing.T) {
	name := mutagen.SessionName("mybox", "./src", "~/src")

	if len(name) == 0 {
		t.Fatal("session name should not be empty")
	}

	// Should start with codebox-<boxname>-
	prefix := "codebox-mybox-"
	if name[:len(prefix)] != prefix {
		t.Errorf("session name should start with %q, got %q", prefix, name)
	}

	// Hash suffix should be 8 hex chars.
	suffix := name[len(prefix):]
	if len(suffix) != 8 {
		t.Errorf("hash suffix should be 8 chars, got %d: %q", len(suffix), suffix)
	}
}

func TestBoxLabelSelector(t *testing.T) {
	got := mutagen.BoxLabelSelector("mybox")
	want := "codebox-box=mybox"

	if got != want {
		t.Errorf("BoxLabelSelector() = %q, want %q", got, want)
	}
}

func TestParsePathPair_Valid(t *testing.T) {
	tests := []struct {
		input      string
		wantLocal  string
		wantRemote string
	}{
		{"./src:~/src", "./src", "~/src"},
		{"/abs/local:/abs/remote", "/abs/local", "/abs/remote"},
		{"./a:b", "./a", "b"},
	}

	for _, tt := range tests {
		local, remote, err := mutagen.ParsePathPair(tt.input)
		if err != nil {
			t.Errorf("ParsePathPair(%q): unexpected error: %v", tt.input, err)
			continue
		}

		if local != tt.wantLocal {
			t.Errorf("ParsePathPair(%q) local = %q, want %q", tt.input, local, tt.wantLocal)
		}

		if remote != tt.wantRemote {
			t.Errorf("ParsePathPair(%q) remote = %q, want %q", tt.input, remote, tt.wantRemote)
		}
	}
}

func TestParsePathPair_NoColon(t *testing.T) {
	_, _, err := mutagen.ParsePathPair("nocolon")
	if err == nil {
		t.Fatal("expected error for input without colon")
	}
}

func TestParsePathPair_EmptyLocal(t *testing.T) {
	_, _, err := mutagen.ParsePathPair(":~/remote")
	if err == nil {
		t.Fatal("expected error for empty local path")
	}
}

func TestParsePathPair_EmptyRemote(t *testing.T) {
	_, _, err := mutagen.ParsePathPair("./local:")
	if err == nil {
		t.Fatal("expected error for empty remote path")
	}
}

func TestRemoteEndpoint(t *testing.T) {
	got := mutagen.RemoteEndpoint("codebox-mybox", "~/project")
	want := "codebox-mybox:~/project"

	if got != want {
		t.Errorf("RemoteEndpoint() = %q, want %q", got, want)
	}
}
