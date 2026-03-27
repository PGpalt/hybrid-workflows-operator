package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hybridwfv1alpha1 "github.com/PGpalt/hybrid-workflows-operator/api/v1alpha1"
)

func TestSyncTerminalConditions(t *testing.T) {
	reconciler := &HybridWorkflowReconciler{}
	hwf := &hybridwfv1alpha1.HybridWorkflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "sample",
			Generation: 3,
		},
	}

	reconciler.syncTerminalConditions(hwf, hybridwfv1alpha1.HybridWorkflowPhaseRunning, "WorkflowPhaseUpdated", "workflow is running")

	if got := conditionStatus(hwf, "Succeeded"); got != metav1.ConditionFalse {
		t.Fatalf("expected Succeeded=False while running, got %s", got)
	}
	if got := conditionStatus(hwf, "Failed"); got != metav1.ConditionFalse {
		t.Fatalf("expected Failed=False while running, got %s", got)
	}

	reconciler.syncTerminalConditions(hwf, hybridwfv1alpha1.HybridWorkflowPhaseSucceeded, "WorkflowPhaseUpdated", "workflow succeeded")

	if got := conditionStatus(hwf, "Succeeded"); got != metav1.ConditionTrue {
		t.Fatalf("expected Succeeded=True after success, got %s", got)
	}
	if got := conditionStatus(hwf, "Failed"); got != metav1.ConditionFalse {
		t.Fatalf("expected Failed=False after success, got %s", got)
	}

	reconciler.syncTerminalConditions(hwf, hybridwfv1alpha1.HybridWorkflowPhaseError, "CompileFailed", "compile failed")

	if got := conditionStatus(hwf, "Succeeded"); got != metav1.ConditionFalse {
		t.Fatalf("expected Succeeded=False after error, got %s", got)
	}
	if got := conditionStatus(hwf, "Failed"); got != metav1.ConditionTrue {
		t.Fatalf("expected Failed=True after error, got %s", got)
	}
}

func conditionStatus(hwf *hybridwfv1alpha1.HybridWorkflow, conditionType string) metav1.ConditionStatus {
	for _, condition := range hwf.Status.Conditions {
		if condition.Type == conditionType {
			return condition.Status
		}
	}
	return metav1.ConditionUnknown
}
