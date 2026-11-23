package sshclient

import (
	"reflect"
	"testing"
)

func TestCommandRecorderBasicAndEditing(t *testing.T) {
	var recorded []string
	rec := NewCommandRecorder(func(cmd string) {
		recorded = append(recorded, cmd)
	})

	rec.Feed([]byte("docker ps -a\r"))
	rec.Feed([]byte("apt updaet\x7f\x7fte\r"))
	rec.Feed([]byte("rm -rf /tmp/oops\x03"))
	rec.Feed([]byte("wrong\x15systemctl status nginx\n"))
	rec.Feed([]byte("ls -l\x1b[Da\r"))
	rec.Feed([]byte("\x1b[200~apk update\napk add curl\n\x1b[201~"))
	rec.Feed([]byte("git checkout wrong\x17main\r"))

	want := []string{
		"docker ps -a",
		"apt update",
		"systemctl status nginx",
		"ls -al",
		"apk update",
		"apk add curl",
		"git checkout main",
	}
	if !reflect.DeepEqual(recorded, want) {
		t.Fatalf("recorded mismatch:\ngot:  %#v\nwant: %#v", recorded, want)
	}
}

func TestCommandRecorderCurrentLineAndUpArrow(t *testing.T) {
	var recorded []string
	rec := NewCommandRecorder(func(cmd string) {
		recorded = append(recorded, cmd)
	})

	rec.Feed([]byte("dock"))
	if got := rec.CurrentLine(); got != "dock" {
		t.Fatalf("expected CurrentLine 'dock', got %q", got)
	}

	rec.Feed([]byte("\x1b[A\r"))
	if len(recorded) != 0 {
		t.Fatalf("expected no recorded command on up-arrow history recall, got %#v", recorded)
	}

	rec.Feed([]byte("free -h\r"))
	if len(recorded) != 1 || recorded[0] != "free -h" {
		t.Fatalf("expected ['free -h'], got %#v", recorded)
	}
}
