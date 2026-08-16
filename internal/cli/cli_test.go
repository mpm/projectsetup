package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunHelp(t *testing.T) {
	var stdout bytes.Buffer
	if err := Run([]string{"--help"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "projectsetup init") {
		t.Fatalf("help output does not describe init:\n%s", stdout.String())
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	err := Run([]string{"generate"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), `unknown command "generate"`) {
		t.Fatalf("Run() error = %v", err)
	}
}
