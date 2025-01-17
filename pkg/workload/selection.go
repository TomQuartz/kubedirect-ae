package workload

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// We use deployment "Namespace/Name" as key to index client workers, gateway dispatchers, and autoscalers
// The passed obj can be Deployment, Service, KnService, or Pod
// The only universal identifier for a general "deployment" is the "app" label
// Therefore the "app" label must match deployment name
func KeyFromObject(obj metav1.Object) string {
	return fmt.Sprintf("%s/%s", obj.GetNamespace(), obj.GetLabels()["app"])
}

func NamespacedNameFromKey(key string) types.NamespacedName {
	parts := strings.Split(key, "/")
	return types.NamespacedName{Namespace: parts[0], Name: parts[1]}
}

func IsWorkload(obj metav1.Object) bool {
	return obj.GetLabels()["app"] != "" && obj.GetLabels()["workload"] != ""
}

var CtrlListOptions = []client.ListOption{
	client.HasLabels{"workload", "app"},
}

var MetaV1ListOptions metav1.ListOptions

func IsTraceWorkload(obj metav1.Object) bool {
	return IsWorkload(obj) && obj.GetLabels()["workload"] == "trace"
}

var CtrlListOptionsForTrace = []client.ListOption{
	client.HasLabels{"workload", "app"},
	client.MatchingLabels{"workload": "trace"},
}

var MetaV1ListOptionsForTrace metav1.ListOptions

func init() {
	check := func(err error) {
		if err != nil {
			panic(err)
		}
	}

	requireWorkload, err := labels.NewRequirement("workload", selection.Exists, nil)
	check(err)
	requireApp, err := labels.NewRequirement("app", selection.Exists, nil)
	check(err)

	MetaV1ListOptionsForTrace = metav1.ListOptions{
		LabelSelector: labels.NewSelector().Add(*requireWorkload, *requireApp).String(),
	}

	requireTraceWorkload, err := labels.NewRequirement("workload", selection.Equals, []string{"trace"})
	check(err)

	MetaV1ListOptionsForTrace = metav1.ListOptions{
		LabelSelector: labels.NewSelector().Add(*requireTraceWorkload, *requireApp).String(),
	}
}
