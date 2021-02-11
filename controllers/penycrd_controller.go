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

const penyLabel = "test"

const (
	// Interval of synchronizing service status from apiserver
	serviceSyncPeriod = 30 * time.Second
	// Interval of synchronizing node status from apiserver
	nodeSyncPeriod = 100 * time.Second

	// How long to wait before retrying the processing of a service change.
	// If this changes, the sleep in hack/jenkins/e2e.sh before downing a cluster
	// should be changed appropriately.
	minRetryDelay = 5 * time.Second
	maxRetryDelay = 300 * time.Second

	// labelNodeRoleMaster specifies that a node is a master. The use of this label within the
	// controller is deprecated and only considered when the LegacyNodeRoleBehavior feature gate
	// is on.
	labelNodeRoleMaster = "node-role.kubernetes.io/master"

	// labelNodeRoleExcludeBalancer specifies that the node should not be considered as a target
	// for external load-balancers which use nodes as a second hop (e.g. many cloud LBs which only
	// understand nodes). For services that use externalTrafficPolicy=Local, this may mean that
	// any backends on excluded nodes are not reachable by those external load-balancers.
	// Implementations of this exclusion may vary based on provider. This label is honored starting
	// in 1.16 when the ServiceNodeExclusion gate is on.
	labelNodeRoleExcludeBalancer = "node.kubernetes.io/exclude-from-external-load-balancers"

	// labelAlphaNodeRoleExcludeBalancer specifies that the node should be
	// exclude from load balancers created by a cloud provider. This label is deprecated and will
	// be removed in 1.18.
	labelAlphaNodeRoleExcludeBalancer = "alpha.service-controller.kubernetes.io/exclude-balancer"

	// serviceNodeExclusionFeature is the feature gate name that
	// enables nodes to exclude themselves from service load balancers
	// originated from: https://github.com/kubernetes/kubernetes/blob/28e800245e/pkg/features/kube_features.go#L178
	serviceNodeExclusionFeature = "ServiceNodeExclusion"

	// legacyNodeRoleBehaviorFeature is the feature gate name that enables legacy
	// behavior to vary cluster functionality on the node-role.kubernetes.io
	// labels.
	legacyNodeRoleBehaviorFeature = "LegacyNodeRoleBehavior"
)

var knownHostsMap map[string]*corev1.Node // node cache as map
var knownHosts []*corev1.Node

// +kubebuilder:rbac:groups=godpeny.peny.k8s.com,resources=penycrds,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=godpeny.peny.k8s.com,resources=penycrds/status,verbs=get;update;patch

func (r *PenyCrdReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
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


	return ctrl.Result{}, nil
}

func (r *PenyCrdReconciler) SetupWithManager(mgr ctrl.Manager) error {

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

					//r.nodeSyncLoop()
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

	if len(knownHosts) == 0 { // initial stage
		fmt.Println("Cache Initial Nodes Info")
		knownHosts = newHosts
		return
	}

	if nodeSlicesEqualForLB(newHosts, knownHosts) {
		// The set of nodes in the cluster hasn't changed, but we can retry
		// updating any services that we failed to update last time around.
		fmt.Println("EQUAL-NO-CHANGE")
		return
	}

	fmt.Println(fmt.Sprintf("Detected change in list of current cluster nodes. New node set: %v", nodeNames(newHosts)))

	// Try updating all services, and save the ones that fail to try again next
	// round.

	knownHosts = newHosts
}

func nodeSlicesEqualForLB(x, y []*v1.Node) bool {

	//fmt.Println("###")
	//fmt.Println(nodeNames(y))
	//fmt.Println(nodeNames(x))
	//fmt.Println("###")

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
