package buildapi

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFindWaiterContainer(t *testing.T) {
	tests := []struct {
		name string
		pod  *corev1.Pod
		want string
	}{
		{
			name: "modern shipwright: regular container running",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "step-build", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{}}},
						{Name: "step-source-local", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
					},
				},
			},
			want: "step-source-local",
		},
		{
			name: "legacy shipwright: init container running",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					InitContainerStatuses: []corev1.ContainerStatus{
						{Name: "step-source-local-upload", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
					},
				},
			},
			want: "step-source-local-upload",
		},
		{
			name: "init container takes priority over regular",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					InitContainerStatuses: []corev1.ContainerStatus{
						{Name: "init-source-local", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
					},
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "step-source-local", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
					},
				},
			},
			want: "init-source-local",
		},
		{
			name: "fallback to any source init container",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					InitContainerStatuses: []corev1.ContainerStatus{
						{Name: "source-fetch", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
					},
				},
			},
			want: "source-fetch",
		},
		{
			name: "no running waiter container",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "step-source-local", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{}}},
					},
				},
			},
			want: "",
		},
		{
			name: "terminated waiter not returned",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "step-source-local", State: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{ExitCode: 0},
						}},
					},
				},
			},
			want: "",
		},
		{
			name: "empty pod",
			pod:  &corev1.Pod{},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findWaiterContainer(tt.pod)
			if got != tt.want {
				t.Errorf("findWaiterContainer() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseWaiterLockFileArg(t *testing.T) {
	tests := []struct {
		name   string
		tokens []string
		want   string
		wantOK bool
	}{
		{
			name:   "equals form",
			tokens: []string{"waiter", "done", "--lock-file=/workspace/.source-done"},
			want:   "/workspace/.source-done",
			wantOK: true,
		},
		{
			name:   "space-separated form",
			tokens: []string{"waiter", "done", "--lock-file", "/tmp/lock"},
			want:   "/tmp/lock",
			wantOK: true,
		},
		{
			name:   "no lock file arg",
			tokens: []string{"waiter", "done"},
			want:   "",
			wantOK: false,
		},
		{
			name:   "empty equals value",
			tokens: []string{"--lock-file="},
			want:   "",
			wantOK: false,
		},
		{
			name:   "empty space value",
			tokens: []string{"--lock-file", ""},
			want:   "",
			wantOK: false,
		},
		{
			name:   "lock-file as last token without value",
			tokens: []string{"--lock-file"},
			want:   "",
			wantOK: false,
		},
		{
			name:   "whitespace-only value ignored",
			tokens: []string{"--lock-file=   "},
			want:   "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotOK := parseWaiterLockFileArg(tt.tokens)
			if got != tt.want || gotOK != tt.wantOK {
				t.Errorf("parseWaiterLockFileArg() = (%q, %v), want (%q, %v)", got, gotOK, tt.want, tt.wantOK)
			}
		})
	}
}

func TestGetWaiterLockFileFromPodSpec(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "step-source-local",
					Command: []string{"waiter"},
					Args:    []string{"done", "--lock-file=/workspace/.done"},
				},
				{
					Name:    "step-build",
					Command: []string{"buildah"},
					Args:    []string{"bud"},
				},
			},
			InitContainers: []corev1.Container{
				{
					Name:    "init-source",
					Command: []string{"waiter"},
					Args:    []string{"done", "--lock-file=/init/.lock"},
				},
			},
		},
	}

	t.Run("finds lock file in regular container", func(t *testing.T) {
		got, ok := getWaiterLockFileFromPodSpec(pod, "step-source-local")
		if !ok || got != "/workspace/.done" {
			t.Errorf("got (%q, %v), want (%q, true)", got, ok, "/workspace/.done")
		}
	})

	t.Run("finds lock file in init container", func(t *testing.T) {
		got, ok := getWaiterLockFileFromPodSpec(pod, "init-source")
		if !ok || got != "/init/.lock" {
			t.Errorf("got (%q, %v), want (%q, true)", got, ok, "/init/.lock")
		}
	})

	t.Run("returns false for non-existent container", func(t *testing.T) {
		_, ok := getWaiterLockFileFromPodSpec(pod, "nonexistent")
		if ok {
			t.Error("expected ok=false for nonexistent container")
		}
	})

	t.Run("returns false for container without lock file", func(t *testing.T) {
		_, ok := getWaiterLockFileFromPodSpec(pod, "step-build")
		if ok {
			t.Error("expected ok=false for container without --lock-file")
		}
	})
}
