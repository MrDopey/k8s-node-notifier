package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	nn "k8s-node-notifier/api/v1"

	"github.com/go-logr/logr"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

type timerValues struct {
	timer *time.Timer
	node  *corev1.Node
	nn    *nn.NodeNotifier
}

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
	timers   = make(map[string]timerValues)
)

const (
	oneHour = int64(3 * 60 * 1e9) // Xmins for debugging
	// oneHour        = int64(60 * 60 * 1e9)
	timerTolerance = int64(float64(oneHour) * 0.05)
)

func init() {
	utilruntime.Must(nn.AddToScheme(scheme))
}

type reconciler struct {
	client.Client
	scheme     *runtime.Scheme
	kubeClient *kubernetes.Clientset
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("nodenotifier", req.NamespacedName)
	log.Info("reconciling nodenotifier")

	var nodeNotifier nn.NodeNotifier
	err := r.Client.Get(ctx, req.NamespacedName, &nodeNotifier)
	timerValue, hasKey := timers[req.Name]

	deleteTimer := func() {
		if hasKey && timerValue.timer != nil {
			// Since we're not tracking the label name
			// Nuke everything and rebuild the timers
			timerValue.timer.Stop()
			log.Info(fmt.Sprintf("Stopped timer for previous node notifier: %s", req.Name))
			delete(timers, req.Name)
		}
	}

	if err != nil {
		if k8serrors.IsNotFound(err) {
			deleteTimer()
		}

		return ctrl.Result{}, err
	}

	deleteTimer()

	err = updateNodeNotifier(&ctx, r.kubeClient, &nodeNotifier, &log)
	if err != nil {
		return ctrl.Result{}, err
	} else {

		return ctrl.Result{}, nil
	}

}

func updateNodeNotifier(ctx *context.Context, clientset *kubernetes.Clientset, nodeNotifier *nn.NodeNotifier, log *logr.Logger) error {

	nodes, err := clientset.CoreV1().Nodes().List(*ctx, v1.ListOptions{LabelSelector: nodeNotifier.Spec.Label})

	if err != nil {
		if k8serrors.IsNotFound(err) {
			timers[nodeNotifier.Name] = timerValues{timer: nil, node: nil, nn: nodeNotifier}
			log.Info(fmt.Sprintf("No nodes found with label '%v' for node notifier name '%v'", nodeNotifier.Spec.Label, nodeNotifier.Name))
			return nil
		} else {
			return err
		}
	} else {
		maxDuration := time.Duration(0)
		var node corev1.Node
		for _, n := range nodes.Items {
			duration := time.Since(n.ObjectMeta.CreationTimestamp.Time)

			if duration > maxDuration {
				maxDuration = duration
				node = n
			}
		}
		if len(nodes.Items) == 0 {
			timers[nodeNotifier.Name] = timerValues{timer: nil, node: nil, nn: nodeNotifier}
			log.Info(fmt.Sprintf("No nodes found with label %s, stopping the watch", nodeNotifier.Spec.Label))
			return nil
		}

		timeUntilNextHour := time.Duration(oneHour - (int64(maxDuration) % oneHour))
		log.Info(fmt.Sprintf("Next tick at %f minutes, for label %s", timeUntilNextHour.Minutes(), nodeNotifier.Spec.Label))
		timer := time.AfterFunc(timeUntilNextHour, checkThenTriggerAlert(ctx, clientset, nodeNotifier, log))

		timers[nodeNotifier.Name] = timerValues{timer: timer, node: &node, nn: nodeNotifier}
		return nil
	}
}

func checkThenTriggerAlert(ctx *context.Context, clientset *kubernetes.Clientset, nodeNotifier *nn.NodeNotifier, log *logr.Logger) func() {
	// Check that nodes are still here and still the latest
	return func() {
		nodes, err := clientset.CoreV1().Nodes().List(*ctx, v1.ListOptions{LabelSelector: nodeNotifier.Spec.Label})

		if err != nil {
			if k8serrors.IsNotFound(err) {
				log.Info(fmt.Sprintf("Node(s) with label '%v' has been decommissioned since last check %s", nodeNotifier.Spec.Label, nodeNotifier.Name))
				timers[nodeNotifier.Name] = timerValues{timer: nil, node: nil, nn: nodeNotifier}
			} else {
				log.Error(err, fmt.Sprintf("Error occurred when fetching nodes for node notifier %s", nodeNotifier.Name))
			}
		} else {
			maxDuration := time.Duration(0)
			var node corev1.Node
			for _, n := range nodes.Items {
				duration := time.Since(n.ObjectMeta.CreationTimestamp.Time)

				if duration > maxDuration {
					maxDuration = duration
					node = n
				}
			}
			if len(nodes.Items) == 0 {
				timers[nodeNotifier.Name] = timerValues{timer: nil, node: nil, nn: nodeNotifier}
				log.Info(fmt.Sprintf("No nodes found with label %s, stopping the watch", nodeNotifier.Spec.Label))
				return
			}
			timeUntilNextHour := oneHour - (int64(maxDuration) % oneHour)
			log.Info(fmt.Sprintf("Next tick at %f minutes, for label %s", time.Duration(timeUntilNextHour).Minutes(), nodeNotifier.Spec.Label))

			if delta := oneHour - timeUntilNextHour; (delta < 0 && delta < -timerTolerance) || (delta > 0 && delta > timerTolerance) {
				// Either, the node we saw last time is retired, and we're looking at another node with the same label
				// or, the timer has drifted long enough to execute this function
				log.Info(fmt.Sprintf("Node for label %s did not trigger at the hour mark for notifier %s", nodeNotifier.Spec, nodeNotifier.Name))
			} else {
				jsonBody := []byte(fmt.Sprintf(`{"text": "node: %s, with label %s, has been running for %f hours"}`, node.Name, nodeNotifier.Spec.Label, maxDuration.Hours()))
				bodyReader := bytes.NewReader(jsonBody)
				res, err := http.Post(nodeNotifier.Spec.SlackUrl, "application/json", bodyReader)
				if err != nil {
					log.Error(err, "error making slack reuqest")
				} else {
					log.Info(fmt.Sprintf("Slack request made with status code %d", res.StatusCode))
				}
			}

			timer := time.AfterFunc(time.Duration(timeUntilNextHour), checkThenTriggerAlert(ctx, clientset, nodeNotifier, log))

			timers[nodeNotifier.Name] = timerValues{timer: timer, node: &node, nn: nodeNotifier}
		}
	}
}

func watchNodes(clientset *kubernetes.Clientset, log *logr.Logger) {
	sendInitialEvent := false
	ctx := context.Background()

	defer log.Error(fmt.Errorf("watchnodes function has exited"), "")

	watcher, err := clientset.CoreV1().Nodes().Watch(ctx, v1.ListOptions{
		AllowWatchBookmarks:  false,
		ResourceVersionMatch: v1.ResourceVersionMatchNotOlderThan,
		SendInitialEvents:    &sendInitialEvent,
	})
	if err != nil {
		panic(err)
	}

	for event := range watcher.ResultChan() {
		item := event.Object.(*corev1.Node)
		log.Info(fmt.Sprintf("Node %s watch event %s", item.Name, event.Type))

		switch event.Type {
		case watch.Bookmark:
			continue
		case watch.Error:
			log.Error(fmt.Errorf("watch event failed for node %s", item.Name), "Error with watching nodes")

		case watch.Deleted:
			for k, v := range timers {
				if v.node.Name == item.Name {
					log.Info(fmt.Sprintf("Was watching node %s for NodeNotifier %s (label %s), but it's been removed. Recalculating next timer.", item.Name, v.nn.Name, v.nn.Spec.Label))
					if v.timer != nil {
						v.timer.Stop()
					}
					// In case something goes wrong, we just keep a null copy to perserve the list of NodeNotifiers
					timers[k] = timerValues{timer: nil, node: nil, nn: v.nn}
					updateNodeNotifier(&ctx, clientset, v.nn, log)
				}
			}

		case watch.Modified: // For minikube, some labels are included after the Added event
			fallthrough
		case watch.Added:
			for labelKey, labelValue := range item.Labels {
				// feature parity with k8s is hard
				// https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#label-selectors
				// Assume the NodeNotifier is a key=value syntax instead
				nodeLabel := fmt.Sprintf("%s=%s", labelKey, labelValue)

				// Fetching CRDs from API is hard
				// Assume the timers map contains all the nodenotifiers
				for nnKey, nnValue := range timers {
					if nnValue.nn.Spec.Label == nodeLabel {
						if nnValue.node == nil {
							log.Info(fmt.Sprintf("New node added %s, that matches the NodeNotifier %s (label %s), starting to track", item.Name, nnValue.nn.Name, nnValue.nn.Spec.Label))
							timer := time.AfterFunc(time.Duration(oneHour), checkThenTriggerAlert(&ctx, clientset, nnValue.nn, log))
							timers[nnKey] = timerValues{timer: timer, node: item, nn: nnValue.nn}

						} else if nnValue.node.Name != item.Name {
							log.Info(fmt.Sprintf("New node added or modified %s, that matches the NodeNotifier %s (label %s), but an older ones is already being tracked %s", item.Name, nnValue.nn.Name, nnValue.nn.Spec.Label, nnValue.node.Name))
						}
					}
					// Don't break
					// A node could potentially match multiple NodeNotifiers
				}
			}

		}
	}
}

func main() {
	var (
		config *rest.Config
		err    error
	)
	kubeconfigFilePath := filepath.Join(homedir.HomeDir(), ".kube", "config")
	if _, err := os.Stat(kubeconfigFilePath); errors.Is(err, os.ErrNotExist) { // if kube config doesn't exist, try incluster config
		config, err = rest.InClusterConfig()
		if err != nil {
			panic(err.Error())
		}
	} else {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfigFilePath)
		if err != nil {
			panic(err.Error())
		}
	}

	// kubernetes client set
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	ctrl.SetLogger(zap.New())

	mgr, err := ctrl.NewManager(config, ctrl.Options{
		Scheme: scheme,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	err = ctrl.NewControllerManagedBy(mgr).
		For(&nn.NodeNotifier{}).
		Complete(&reconciler{
			Client:     mgr.GetClient(),
			scheme:     mgr.GetScheme(),
			kubeClient: clientset,
		})
	if err != nil {
		setupLog.Error(err, "unable to create controller")
		os.Exit(1)
	}

	go watchNodes(clientset, &setupLog)

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "error running manager")
		os.Exit(1)
	}
}
