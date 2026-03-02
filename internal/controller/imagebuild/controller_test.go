package imagebuild

import (
	"strings"
	"testing"

	tektonv1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	knativev1 "knative.dev/pkg/apis/duck/v1"
)

func TestPipelineRunFailureMessage(t *testing.T) {
	tests := []struct {
		name        string
		pipelineRun *tektonv1.PipelineRun
		want        string
	}{
		{
			name: "returns condition message on failure",
			pipelineRun: &tektonv1.PipelineRun{
				Status: tektonv1.PipelineRunStatus{
					Status: knativev1.Status{
						Conditions: knativev1.Conditions{
							{
								Type:    conditionSucceeded,
								Status:  corev1.ConditionFalse,
								Message: "TaskRun build-step failed: container exited with code 1",
							},
						},
					},
				},
			},
			want: "Build failed: TaskRun build-step failed: container exited with code 1",
		},
		{
			name: "returns fallback when no conditions",
			pipelineRun: &tektonv1.PipelineRun{
				Status: tektonv1.PipelineRunStatus{},
			},
			want: "Build failed",
		},
		{
			name: "returns fallback when Succeeded condition has empty message",
			pipelineRun: &tektonv1.PipelineRun{
				Status: tektonv1.PipelineRunStatus{
					Status: knativev1.Status{
						Conditions: knativev1.Conditions{
							{
								Type:   conditionSucceeded,
								Status: corev1.ConditionFalse,
							},
						},
					},
				},
			},
			want: "Build failed",
		},
		{
			name: "ignores non-Succeeded conditions",
			pipelineRun: &tektonv1.PipelineRun{
				Status: tektonv1.PipelineRunStatus{
					Status: knativev1.Status{
						Conditions: knativev1.Conditions{
							{
								Type:    "Ready",
								Status:  corev1.ConditionFalse,
								Message: "not ready",
							},
						},
					},
				},
			},
			want: "Build failed",
		},
		{
			name: "ignores Succeeded=True condition",
			pipelineRun: &tektonv1.PipelineRun{
				Status: tektonv1.PipelineRunStatus{
					Status: knativev1.Status{
						Conditions: knativev1.Conditions{
							{
								Type:    conditionSucceeded,
								Status:  corev1.ConditionTrue,
								Message: "All Tasks have completed executing",
							},
						},
					},
				},
			},
			want: "Build failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pipelineRunFailureMessage(tt.pipelineRun)
			if got != tt.want {
				t.Errorf("pipelineRunFailureMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTaskRunFailureMessage(t *testing.T) {
	tests := []struct {
		name     string
		taskRun  *tektonv1.TaskRun
		fallback string
		want     string
	}{
		{
			name: "returns condition message on failure",
			taskRun: &tektonv1.TaskRun{
				Status: tektonv1.TaskRunStatus{
					Status: knativev1.Status{
						Conditions: knativev1.Conditions{
							{
								Type:    conditionSucceeded,
								Status:  corev1.ConditionFalse,
								Message: "step flash failed: timeout waiting for device",
							},
						},
					},
				},
			},
			fallback: "Flash to device failed",
			want:     "Flash to device failed: step flash failed: timeout waiting for device",
		},
		{
			name: "returns fallback when no conditions",
			taskRun: &tektonv1.TaskRun{
				Status: tektonv1.TaskRunStatus{},
			},
			fallback: "Flash to device failed",
			want:     "Flash to device failed",
		},
		{
			name: "returns fallback when Succeeded condition has empty message",
			taskRun: &tektonv1.TaskRun{
				Status: tektonv1.TaskRunStatus{
					Status: knativev1.Status{
						Conditions: knativev1.Conditions{
							{
								Type:   conditionSucceeded,
								Status: corev1.ConditionFalse,
							},
						},
					},
				},
			},
			fallback: "Flash to device failed",
			want:     "Flash to device failed",
		},
		{
			name: "ignores Succeeded=True condition",
			taskRun: &tektonv1.TaskRun{
				Status: tektonv1.TaskRunStatus{
					Status: knativev1.Status{
						Conditions: knativev1.Conditions{
							{
								Type:    conditionSucceeded,
								Status:  corev1.ConditionTrue,
								Message: "All steps completed",
							},
						},
					},
				},
			},
			fallback: "Flash to device failed",
			want:     "Flash to device failed",
		},
		{
			name: "uses custom fallback message",
			taskRun: &tektonv1.TaskRun{
				Status: tektonv1.TaskRunStatus{},
			},
			fallback: "Custom operation failed",
			want:     "Custom operation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := taskRunFailureMessage(tt.taskRun, tt.fallback)
			if got != tt.want {
				t.Errorf("taskRunFailureMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSafeDerivedName(t *testing.T) {
	tests := []struct {
		name       string
		baseName   string
		suffix     string
		wantMaxLen int
		wantSuffix string
	}{
		{
			name:       "short name no truncation",
			baseName:   "simple",
			suffix:     "-manifest",
			wantMaxLen: 63,
			wantSuffix: "-manifest",
		},
		{
			name:       "exact length boundary",
			baseName:   "exactly-fifty-four-chars-to-test-boundary-conditions",
			suffix:     "-manifest",
			wantMaxLen: 63,
			wantSuffix: "-manifest",
		},
		{
			name:       "long name needs truncation",
			baseName:   "this-is-a-very-long-build-name-that-will-definitely-exceed-limits",
			suffix:     "-manifest",
			wantMaxLen: 63,
			wantSuffix: "-manifest",
		},
		{
			name:       "long suffix",
			baseName:   "build-name",
			suffix:     "-upload-pod",
			wantMaxLen: 63,
			wantSuffix: "-upload-pod",
		},
		{
			name:       "very long name with short suffix",
			baseName:   "extremely-long-build-name-that-definitely-exceeds-kubernetes-dns-label-limits",
			suffix:     "-ws",
			wantMaxLen: 63,
			wantSuffix: "-ws",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := safeDerivedName(tt.baseName, tt.suffix)

			// Check length constraint
			if len(result) > tt.wantMaxLen {
				t.Errorf("safeDerivedName() result %q length %d exceeds max %d", result, len(result), tt.wantMaxLen)
			}

			// Check suffix is preserved
			if !strings.HasSuffix(result, tt.wantSuffix) {
				t.Errorf("safeDerivedName() result %q does not end with expected suffix %q", result, tt.wantSuffix)
			}

			// Check deterministic (same input gives same output)
			result2 := safeDerivedName(tt.baseName, tt.suffix)
			if result != result2 {
				t.Errorf("safeDerivedName() is not deterministic: %q != %q", result, result2)
			}
		})
	}

	// Test uniqueness: different base names that would truncate to same prefix should produce different results
	t.Run("hash provides uniqueness", func(t *testing.T) {
		longName1 := "very-long-name-with-same-prefix-but-different-suffix-one"
		longName2 := "very-long-name-with-same-prefix-but-different-suffix-two"
		suffix := "-manifest"

		result1 := safeDerivedName(longName1, suffix)
		result2 := safeDerivedName(longName2, suffix)

		if result1 == result2 {
			t.Errorf("safeDerivedName() produced same result for different inputs: %q", result1)
		}

		// Both should still be valid length and have correct suffix
		if len(result1) > 63 || len(result2) > 63 {
			t.Errorf("safeDerivedName() results exceed length: %q (%d), %q (%d)", result1, len(result1), result2, len(result2))
		}

		if !strings.HasSuffix(result1, suffix) || !strings.HasSuffix(result2, suffix) {
			t.Errorf("safeDerivedName() results don't have correct suffix: %q, %q", result1, result2)
		}
	})
}
