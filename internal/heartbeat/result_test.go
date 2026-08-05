package heartbeat_test

import (
	"testing"

	"github.com/jfox85/supawake/internal/heartbeat"
)

func TestExitCodeContract(t *testing.T) {
	tests := []struct {
		name    string
		results []heartbeat.Result
		want    int
	}{
		{"none", nil, 0},
		{"all healthy", []heartbeat.Result{{Status: heartbeat.Healthy}}, 0},
		{"one failed", []heartbeat.Result{{Status: heartbeat.Healthy}, {Status: heartbeat.ProjectPaused}}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := heartbeat.ExitCode(tt.results); got != tt.want {
				t.Fatalf("ExitCode() = %d, want %d", got, tt.want)
			}
		})
	}
}
