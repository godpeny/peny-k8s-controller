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
	"reflect"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	runtimeutil "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	// APis added
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
)

// PenyCrdReconciler reconciles a PenyCrd object
type PenyCrdReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

const (
	penyLabel      = "test"
	nodeSyncPeriod = 10 * time.Second
)

var knownHostsMap map[string]*corev1.Node // node cache as map
var sync bool                             // go routine doesn't occur when sync == true

// +kubebuilder:rbac:groups=godpeny.peny.k8s.com,resources=penycrds,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=godpeny.peny.k8s.com,resources=penycrds/status,verbs=get;update;patch

func (r *PenyCrdReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	sync = true

	ctx := context.Background()
	reqLogger := r.Log.WithValues("penycrd", req.NamespacedName)

	node := &corev1.Node{}
	err := r.Client.Get(ctx, req.NamespacedName, node)
	if err != nil {
		if errors.IsNotFound(err) {
			// node deleted
			fmt.Println("DELETE NODE DETECTED")
			delete(knownHostsMap, req.Name)
			return ctrl.Result{}, nil
		} else {
			reqLogger.Error(err, "Failed to get node.")
			return ctrl.Result{}, err
		}
	}
	reqLogger.Info("Reconcile")

	if len(knownHostsMap) == 0 {
		knownHostsMap = make(map[string]*corev1.Node)
	}

	if _, found := knownHostsMap[node.Name]; found {
		// label updated
	} else {
		// node added
		knownHostsMap[node.Name] = node
	}

	sync = false

	return ctrl.Result{}, nil
}

func (r *PenyCrdReconciler) SetupWithManager(mgr ctrl.Manager) error {

	stopCh := make(chan struct{})

	go wait.Until(func() {
		select {
		case <-mgr.Elected():
			klog.V(4).Info("nodeSyncLoop")
			r.nodeSyncLoop()
		default:
		}
	}, nodeSyncPeriod, stopCh)

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}).
		WithEventFilter(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				fmt.Println("CREATE")
				return true
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				// Add event filter logic to reconcile

				fmt.Println(nodeNames(convertMapToSlice(knownHostsMap)))

				ol := e.MetaOld.GetLabels()
				nl := e.MetaNew.GetLabels()
				eq := reflect.DeepEqual(ol, nl)

				if !eq && findKeyInMap(penyLabel, nl) {

					fmt.Println(e.MetaNew.GetName())
					fmt.Println("Labels Updated! They're unequal.")
					fmt.Println(e.MetaOld.GetLabels())
					fmt.Println(e.MetaNew.GetLabels())

					return true
				}
				return false
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				// Add event filter logic to reconcile
				fmt.Println("DELETE")
				return true
			},
		}).
		Complete(r)
}

func findKeyInMap(s string, m map[string]string) bool {
	if _, found := m[s]; found {
		return true
	}
	return false
}

func (r *PenyCrdReconciler) nodeSyncLoop() {
	if sync { // if in reconcilation phase , skip and try next round.
		return
	}

	nodeList := &corev1.NodeList{}
	err := r.Client.List(context.TODO(), nodeList)
	if err != nil {
		fmt.Println(fmt.Errorf("Failed to retrieve current set of nodes from node lister: %v", err))
		return
	}

	nodes := convertToList(nodeList)
	newHosts, err := listWithPredicate(nodes, getNodeConditionPredicate())
	if err != nil {
		runtimeutil.HandleError(fmt.Errorf("Failed to retrieve current set of nodes from node lister: %v", err))
		return
	}

	knownHosts := convertMapToSlice(knownHostsMap)

	if nodeSlicesEqualForLB(newHosts, knownHosts) {
		// The set of nodes in the cluster hasn't changed, but we can retry
		// updating any services that we failed to update last time around.
		fmt.Println("EQUAL-NO-CHANGE")

		for i, v := range newHosts {
			fmt.Println(i, " : ", v.Status.Addresses)
		}

		return
	}

	fmt.Println(fmt.Sprintf("Detected change in list of current cluster nodes. New node set: %v", nodeNames(newHosts)))
	// update cache - knownHostsMap
	return
}

func nodeSlicesEqualForLB(x, y []*v1.Node) bool {
	if len(x) != len(y) {
		return false
	}

	return nodeNames(x).Equal(nodeNames(y))
}

func nodeNames(nodes []*v1.Node) sets.String {
	ret := sets.NewString()
	for _, node := range nodes {
		ret.Insert(node.Name)
	}
	return ret
}

func convertToList(nodeList *corev1.NodeList) []*corev1.Node {
	var nodes []*corev1.Node
	for i := range nodeList.Items {
		nodes = append(nodes, &nodeList.Items[i])
	}

	return nodes
}

// NodeConditionPredicate is a function that indicates whether the given node's conditions meet
// some set of criteria defined by the function.
type NodeConditionPredicate func(node *corev1.Node) bool

// listWithPredicate gets nodes that matches predicate function.
func listWithPredicate(nodes []*corev1.Node, predicate NodeConditionPredicate) ([]*corev1.Node, error) {

	var filtered []*corev1.Node
	for i := range nodes {
		if predicate(nodes[i]) {
			filtered = append(filtered, nodes[i])
		}
	}

	return filtered, nil
}

func getNodeConditionPredicate() NodeConditionPredicate {
	return func(node *v1.Node) bool {
		// If we have no info, don't accept
		if len(node.Status.Conditions) == 0 {
			return false
		}
		for _, cond := range node.Status.Conditions {
			// We consider the node for load balancing only when its NodeReady condition status
			// is ConditionTrue
			if cond.Type == v1.NodeReady && cond.Status != v1.ConditionTrue {
				klog.V(4).Infof("Ignoring node %v with %v condition status %v", node.Name, cond.Type, cond.Status)
				return false
			}
		}
		return true
	}
}

func convertMapToSlice(m map[string]*corev1.Node) []*corev1.Node {
	v := make([]*corev1.Node, 0, len(m))

	for _, value := range m {
		v = append(v, value)
	}
	return v
}
