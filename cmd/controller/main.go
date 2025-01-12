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

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
	timers   = make(map[string]*time.Timer)
)

const (
	// oneHour = int64(3 * 60 * 1e9) // 5mins for debugging
	oneHour        = int64(60 * 60 * 1e9)
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
	timer, hasKey := timers[req.Name]

	deleteTimer := func() {
		if hasKey {
			// Since we're not tracking the label name
			// Nuke everything and rebuild the timers
			timer.Stop()
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

	nodes, err := r.kubeClient.CoreV1().Nodes().List(ctx, v1.ListOptions{LabelSelector: nodeNotifier.Spec.Label})

	if err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info(fmt.Sprintf("No nodes found with label '%v' for node notifier name '%v'", nodeNotifier.Spec.Label, req.Name))
			return ctrl.Result{}, nil
		} else {
			return ctrl.Result{}, err
		}
	} else {
		maxDuration := time.Duration(0)
		for _, n := range nodes.Items {
			duration := time.Since(n.ObjectMeta.CreationTimestamp.Time)

			if duration > maxDuration {
				maxDuration = duration
			}
		}
		timeUntilNextHour := time.Duration(oneHour - (int64(maxDuration) % oneHour))
		log.Info(fmt.Sprintf("Next tick at %f minutes, for label %s", timeUntilNextHour.Minutes(), nodeNotifier.Spec.Label))
		timer := time.AfterFunc(timeUntilNextHour, checkThenTriggerAlert(r, &ctx, nodeNotifier, &log))

		timers[req.Name] = timer
	}

	return ctrl.Result{}, nil
}

func checkThenTriggerAlert(r *reconciler, ctx *context.Context, nodeNotifier nn.NodeNotifier, log *logr.Logger) func() {
	// Check that nodes are still here and still the latest
	return func() {
		nodes, err := r.kubeClient.CoreV1().Nodes().List(*ctx, v1.ListOptions{LabelSelector: nodeNotifier.Spec.Label})

		if err != nil {
			if k8serrors.IsNotFound(err) {
				log.Info(fmt.Sprintf("Node(s) with label '%v' has been decommissioned since last check %s", nodeNotifier.Spec.Label, nodeNotifier.Name))
			} else {
				log.Error(err, fmt.Sprintf("Error occurred when fetching nodes for node notifier %s", nodeNotifier.Name))
			}
		} else {
			maxDuration, nodeName := time.Duration(0), ""
			for _, n := range nodes.Items {
				duration := time.Since(n.ObjectMeta.CreationTimestamp.Time)

				if duration > maxDuration {
					maxDuration = duration
					nodeName = n.Name
				}
			}
			timeUntilNextHour := oneHour - (int64(maxDuration) % oneHour)
			log.Info(fmt.Sprintf("Next tick at %f minutes, for label %s", time.Duration(timeUntilNextHour).Minutes(), nodeNotifier.Spec.Label))

			if delta := oneHour - timeUntilNextHour; (delta < 0 && delta < -timerTolerance) || (delta > 0 && delta > timerTolerance) {
				// Either, the node we saw last time is retired, and we're looking at another node with the same label
				// or, the timer has drifted long enough to execute this function
				log.Info(fmt.Sprintf("Node for label %s did not trigger at the hour mark for notifier %s", nodeNotifier.Spec, nodeNotifier.Name))
			} else {
				jsonBody := []byte(fmt.Sprintf(`{"text": "node: %s, with label %s, has been running for %f hours"}`, nodeName, nodeNotifier.Spec.Label, maxDuration.Hours()))
				bodyReader := bytes.NewReader(jsonBody)
				res, err := http.Post(nodeNotifier.Spec.SlackUrl, "application/json", bodyReader)
				if err != nil {
					log.Error(err, "error making slack reuqest")
				} else {
					log.Info(fmt.Sprintf("Slack request made with status code %d", res.StatusCode))
				}
			}

			timer := time.AfterFunc(time.Duration(timeUntilNextHour), checkThenTriggerAlert(r, ctx, nodeNotifier, log))

			timers[nodeNotifier.Name] = timer
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

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "error running manager")
		os.Exit(1)
	}
}
