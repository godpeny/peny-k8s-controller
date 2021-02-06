/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	// APis added
	corev1 "k8s.io/api/core/v1"
)

// PenyCrdReconciler reconciles a PenyCrd object
type PenyCrdReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=godpeny.peny.k8s.com,resources=penycrds,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=godpeny.peny.k8s.com,resources=penycrds/status,verbs=get;update;patch

func (r *PenyCrdReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	reqLogger := r.Log.WithValues("penycrd", req.NamespacedName)

	nodeList := &corev1.NodeList{}
	err := r.Client.List(ctx, nodeList)
	if err != nil {
		reqLogger.Error(err, "Failed to list nodes.")
		return ctrl.Result{}, err
	}

	nodeNames := getNodeNames(nodeList.Items)

	fmt.Println("NODE")
	fmt.Println(nodeNames)

	return ctrl.Result{}, nil
}

func (r *PenyCrdReconciler) SetupWithManager(mgr ctrl.Manager) error {

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}).
		WithEventFilter(predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				fmt.Println("UP") // Up on every update event

				// Add event filter logic to reconcile

				return false
			},
		}).
		Complete(r)
}

// getPodNames returns the pod names of the array of pods passed in
func getPodNames(pods []corev1.Pod) []string {
	var podNames []string
	for _, pod := range pods {
		podNames = append(podNames, pod.Name)
	}
	return podNames
}

// getNodeNames returns the pod names of the array of pods passed in
func getNodeNames(pods []corev1.Node) []string {
	var podNames []string
	for _, pod := range pods {
		podNames = append(podNames, pod.Name)
	}
	return podNames
}
