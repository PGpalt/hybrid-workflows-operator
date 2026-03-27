package controller

import (
	"context"
	"fmt"
	"maps"
	"reflect"

	argov1alpha1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	hybridwfv1alpha1 "github.com/PGpalt/hybrid-workflows-operator/api/v1alpha1"
	"github.com/PGpalt/hybrid-workflows-operator/internal/compiler"
)

// HybridWorkflowReconciler reconciles a HybridWorkflow object.
type HybridWorkflowReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=hybridwf.io,resources=hybridworkflows,verbs=get;list;watch
// +kubebuilder:rbac:groups=hybridwf.io,resources=hybridworkflows/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hybridwf.io,resources=hybridworkflows/finalizers,verbs=update
// +kubebuilder:rbac:groups=argoproj.io,resources=workflows,verbs=get;list;watch;create;update;patch;delete

func (r *HybridWorkflowReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var hybridWorkflow hybridwfv1alpha1.HybridWorkflow
	if err := r.Get(ctx, req.NamespacedName, &hybridWorkflow); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	originalStatus := hybridWorkflow.Status

	renderedWorkflow, err := compiler.Compile(&hybridWorkflow)
	if err != nil {
		r.setReadyCondition(&hybridWorkflow, metav1.ConditionFalse, "CompileFailed", err.Error())
		hybridWorkflow.Status.ObservedGeneration = hybridWorkflow.Generation
		hybridWorkflow.Status.Phase = hybridwfv1alpha1.HybridWorkflowPhaseError
		r.syncTerminalConditions(&hybridWorkflow, hybridwfv1alpha1.HybridWorkflowPhaseError, "CompileFailed", err.Error())
		if statusErr := r.patchStatus(ctx, &hybridWorkflow, originalStatus); statusErr != nil {
			return ctrl.Result{}, fmt.Errorf("compile error %v; status patch error: %w", err, statusErr)
		}
		return ctrl.Result{}, err
	}

	workflowName := childWorkflowName(&hybridWorkflow)
	var workflow argov1alpha1.Workflow
	workflow.Namespace = hybridWorkflow.Namespace
	workflow.Name = workflowName

	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, &workflow, func() error {
		workflow.Spec = renderedWorkflow.Spec
		workflow.Labels = mergeStringMaps(workflow.Labels, map[string]string{
			"hybridwf.io/name":       hybridWorkflow.Name,
			"hybridwf.io/generation": fmt.Sprintf("%d", hybridWorkflow.Generation),
		})
		if err := controllerutil.SetControllerReference(&hybridWorkflow, &workflow, r.Scheme); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		r.setReadyCondition(&hybridWorkflow, metav1.ConditionFalse, "WorkflowSyncFailed", err.Error())
		hybridWorkflow.Status.ObservedGeneration = hybridWorkflow.Generation
		hybridWorkflow.Status.Phase = hybridwfv1alpha1.HybridWorkflowPhaseError
		r.syncTerminalConditions(&hybridWorkflow, hybridwfv1alpha1.HybridWorkflowPhaseError, "WorkflowSyncFailed", err.Error())
		if statusErr := r.patchStatus(ctx, &hybridWorkflow, originalStatus); statusErr != nil {
			return ctrl.Result{}, fmt.Errorf("workflow sync error %v; status patch error: %w", err, statusErr)
		}
		return ctrl.Result{}, err
	}

	hybridWorkflow.Status.ObservedGeneration = hybridWorkflow.Generation
	hybridWorkflow.Status.RenderedWorkflowName = workflow.Name
	hybridWorkflow.Status.Phase = mapWorkflowPhase(workflow.Status.Phase)
	r.syncTerminalConditions(&hybridWorkflow, hybridWorkflow.Status.Phase, "WorkflowPhaseUpdated", fmt.Sprintf("workflow %s phase is %s", workflow.Name, hybridWorkflow.Status.Phase))
	r.setReadyCondition(&hybridWorkflow, metav1.ConditionTrue, "Reconciled", fmt.Sprintf("workflow %s reconciled", workflow.Name))

	if err := r.patchStatus(ctx, &hybridWorkflow, originalStatus); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("reconciled hybrid workflow", "hybridWorkflow", hybridWorkflow.Name, "workflow", workflow.Name, "phase", hybridWorkflow.Status.Phase)
	return ctrl.Result{}, nil
}

func (r *HybridWorkflowReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hybridwfv1alpha1.HybridWorkflow{}).
		Owns(&argov1alpha1.Workflow{}).
		Complete(r)
}

func (r *HybridWorkflowReconciler) patchStatus(
	ctx context.Context,
	hwf *hybridwfv1alpha1.HybridWorkflow,
	original hybridwfv1alpha1.HybridWorkflowStatus,
) error {
	if reflect.DeepEqual(hwf.Status, original) {
		return nil
	}
	return r.Status().Update(ctx, hwf)
}

func (r *HybridWorkflowReconciler) setReadyCondition(
	hwf *hybridwfv1alpha1.HybridWorkflow,
	status metav1.ConditionStatus,
	reason, message string,
) {
	r.setCondition(hwf, "Ready", status, reason, message)
}

func (r *HybridWorkflowReconciler) setCondition(
	hwf *hybridwfv1alpha1.HybridWorkflow,
	conditionType string,
	status metav1.ConditionStatus,
	reason, message string,
) {
	now := metav1.Now()
	for i := range hwf.Status.Conditions {
		if hwf.Status.Conditions[i].Type != conditionType {
			continue
		}
		if hwf.Status.Conditions[i].Status != status ||
			hwf.Status.Conditions[i].Reason != reason ||
			hwf.Status.Conditions[i].Message != message {
			hwf.Status.Conditions[i].LastTransitionTime = now
		}
		hwf.Status.Conditions[i].Status = status
		hwf.Status.Conditions[i].Reason = reason
		hwf.Status.Conditions[i].Message = message
		hwf.Status.Conditions[i].ObservedGeneration = hwf.Generation
		return
	}

	hwf.Status.Conditions = append(hwf.Status.Conditions, hybridwfv1alpha1.HybridWorkflowCondition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: hwf.Generation,
		LastTransitionTime: now,
	})
}

func (r *HybridWorkflowReconciler) syncTerminalConditions(
	hwf *hybridwfv1alpha1.HybridWorkflow,
	phase hybridwfv1alpha1.HybridWorkflowPhase,
	reason, message string,
) {
	switch phase {
	case hybridwfv1alpha1.HybridWorkflowPhaseSucceeded:
		r.setCondition(hwf, "Succeeded", metav1.ConditionTrue, reason, message)
		r.setCondition(hwf, "Failed", metav1.ConditionFalse, "NotFailed", "workflow has not failed")
	case hybridwfv1alpha1.HybridWorkflowPhaseFailed, hybridwfv1alpha1.HybridWorkflowPhaseError:
		r.setCondition(hwf, "Succeeded", metav1.ConditionFalse, "NotSucceeded", "workflow has not succeeded")
		r.setCondition(hwf, "Failed", metav1.ConditionTrue, reason, message)
	default:
		r.setCondition(hwf, "Succeeded", metav1.ConditionFalse, "NotSucceeded", "workflow has not succeeded")
		r.setCondition(hwf, "Failed", metav1.ConditionFalse, "NotFailed", "workflow has not failed")
	}
}

func childWorkflowName(hwf *hybridwfv1alpha1.HybridWorkflow) string {
	return fmt.Sprintf("%s-workflow", hwf.Name)
}

func mapWorkflowPhase(phase argov1alpha1.WorkflowPhase) hybridwfv1alpha1.HybridWorkflowPhase {
	switch phase {
	case argov1alpha1.WorkflowPending:
		return hybridwfv1alpha1.HybridWorkflowPhasePending
	case argov1alpha1.WorkflowRunning:
		return hybridwfv1alpha1.HybridWorkflowPhaseRunning
	case argov1alpha1.WorkflowSucceeded:
		return hybridwfv1alpha1.HybridWorkflowPhaseSucceeded
	case argov1alpha1.WorkflowFailed:
		return hybridwfv1alpha1.HybridWorkflowPhaseFailed
	case argov1alpha1.WorkflowError:
		return hybridwfv1alpha1.HybridWorkflowPhaseError
	default:
		return hybridwfv1alpha1.HybridWorkflowPhaseSubmitted
	}
}

func mergeStringMaps(existing, desired map[string]string) map[string]string {
	if existing == nil && desired == nil {
		return nil
	}
	merged := map[string]string{}
	maps.Copy(merged, existing)
	maps.Copy(merged, desired)
	return merged
}
