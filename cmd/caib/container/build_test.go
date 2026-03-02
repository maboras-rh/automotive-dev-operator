package container

import (
	"testing"
)

func TestParseContainerBuildArgs(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		want    map[string]string
		wantLen int
	}{
		{
			name:    "single arg",
			input:   []string{"VERSION=1.0"},
			want:    map[string]string{"VERSION": "1.0"},
			wantLen: 1,
		},
		{
			name:    "multiple args",
			input:   []string{"VERSION=1.0", "ENV=prod"},
			want:    map[string]string{"VERSION": "1.0", "ENV": "prod"},
			wantLen: 2,
		},
		{
			name:    "value with equals sign",
			input:   []string{"CMD=echo foo=bar"},
			want:    map[string]string{"CMD": "echo foo=bar"},
			wantLen: 1,
		},
		{
			name:    "empty input",
			input:   []string{},
			want:    map[string]string{},
			wantLen: 0,
		},
		{
			name:    "value with spaces",
			input:   []string{"MSG=hello world"},
			want:    map[string]string{"MSG": "hello world"},
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseContainerBuildArgs(tt.input)
			if len(got) != tt.wantLen {
				t.Errorf("parseContainerBuildArgs() returned %d args, want %d", len(got), tt.wantLen)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("parseContainerBuildArgs()[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestContainerBuildPhaseStep(t *testing.T) {
	tests := []struct {
		phase string
		want  int
	}{
		{phasePending, 0},
		{phaseUploading, 1},
		{"Building", 2},
		{phaseCompleted, containerBuildTotalSteps},
		{phaseFailed, containerBuildTotalSteps},
		{"Unknown", 1}, // default
	}

	for _, tt := range tests {
		t.Run(tt.phase, func(t *testing.T) {
			got := containerBuildPhaseStep(tt.phase)
			if got != tt.want {
				t.Errorf("containerBuildPhaseStep(%q) = %d, want %d", tt.phase, got, tt.want)
			}
		})
	}
}

func TestIsContainerBuildTerminal(t *testing.T) {
	tests := []struct {
		phase string
		want  bool
	}{
		{phaseCompleted, true},
		{phaseFailed, true},
		{phasePending, false},
		{phaseUploading, false},
		{"Building", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.phase, func(t *testing.T) {
			got := isContainerBuildTerminal(tt.phase)
			if got != tt.want {
				t.Errorf("isContainerBuildTerminal(%q) = %v, want %v", tt.phase, got, tt.want)
			}
		})
	}
}
