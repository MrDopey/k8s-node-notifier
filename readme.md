# Pre-amble

Running workloads in your k8s cluster on an auto-scaling nodepool, but people keep leaving things running? e.g. Jupyter notebook, or `kube-system` pods accidentally get moved to your auto-scaling node (I'm looking at you metrics-server). 

This is an attempt to gain visibility to the k8s cluster nodes and prevent bill-shock.

It is an operator that looks at the node's creation time and shoots a slack message every hour they are running grouped by a label selector.

Note: An alternative method could be to view this at a node level, and implemented as a DaemonSet, though that depends on your use case, as this version looks at a grouping of nodes. This is mostly for me to get familiar with writing a k8s operator


# Sample logs

With reporting at 3minute intervals

```shell
{"level":"info","ts":"2025-01-14T12:33:16Z","msg":"reconciling nodenotifier","controller":"nodenotifier","controllerGroup":"mr.dopey","controllerKind":"NodeNotifier","NodeNotifier":{"name":"primary"},"namespace":"","name":"primary","reconcileID":"5eb0d366-044d-4284-860a-290917218f98","nodenotifier":{"name":"primary"}}
{"level":"info","ts":"2025-01-14T12:33:16Z","msg":"Next tick at 2.140002 minutes, for label minikube.k8s.io/primary=true","controller":"nodenotifier","controllerGroup":"mr.dopey","controllerKind":"NodeNotifier","NodeNotifier":{"name":"primary"},"namespace":"","name":"primary","reconcileID":"5eb0d366-044d-4284-860a-290917218f98","nodenotifier":{"name":"primary"}}
{"level":"info","ts":"2025-01-14T12:33:16Z","msg":"reconciling nodenotifier","controller":"nodenotifier","controllerGroup":"mr.dopey","controllerKind":"NodeNotifier","NodeNotifier":{"name":"secondary"},"namespace":"","name":"secondary","reconcileID":"66cff377-c197-4a9d-8d15-ed43e2c59a91","nodenotifier":{"name":"secondary"}}
{"level":"info","ts":"2025-01-14T12:33:16Z","msg":"No nodes found with label minikube.k8s.io/primary=false, stopping the watch","controller":"nodenotifier","controllerGroup":"mr.dopey","controllerKind":"NodeNotifier","NodeNotifier":{"name":"secondary"},"namespace":"","name":"secondary","reconcileID":"66cff377-c197-4a9d-8d15-ed43e2c59a91","nodenotifier":{"name":"secondary"}}
{"level":"info","ts":"2025-01-14T12:35:25Z","msg":"Next tick at 2.999845 minutes, for label minikube.k8s.io/primary=true","controller":"nodenotifier","controllerGroup":"mr.dopey","controllerKind":"NodeNotifier","NodeNotifier":{"name":"primary"},"namespace":"","name":"primary","reconcileID":"5eb0d366-044d-4284-860a-290917218f98","nodenotifier":{"name":"primary"}}
{"level":"info","ts":"2025-01-14T12:35:25Z","msg":"Slack request made with status code 200","controller":"nodenotifier","controllerGroup":"mr.dopey","controllerKind":"NodeNotifier","NodeNotifier":{"name":"primary"},"namespace":"","name":"primary","reconcileID":"5eb0d366-044d-4284-860a-290917218f98","nodenotifier":{"name":"primary"}}
{"level":"info","ts":"2025-01-14T12:35:51Z","logger":"setup","msg":"Node minikube-m02 watch event ADDED"}
{"level":"info","ts":"2025-01-14T12:35:51Z","logger":"setup","msg":"Node minikube-m02 watch event MODIFIED"}
{"level":"info","ts":"2025-01-14T12:35:51Z","logger":"setup","msg":"Node minikube-m02 watch event MODIFIED"}
{"level":"info","ts":"2025-01-14T12:35:51Z","logger":"setup","msg":"Node minikube-m02 watch event MODIFIED"}
{"level":"info","ts":"2025-01-14T12:35:51Z","logger":"setup","msg":"Node minikube-m02 watch event MODIFIED"}
{"level":"info","ts":"2025-01-14T12:35:51Z","logger":"setup","msg":"New node added minikube-m02, that matches the NodeNotifier secondary (label minikube.k8s.io/primary=false), starting to track"}
{"level":"info","ts":"2025-01-14T12:35:52Z","logger":"setup","msg":"Node minikube-m02 watch event MODIFIED"}
{"level":"info","ts":"2025-01-14T12:35:52Z","logger":"setup","msg":"Node minikube-m02 watch event MODIFIED"}
{"level":"info","ts":"2025-01-14T12:35:57Z","logger":"setup","msg":"Node minikube watch event MODIFIED"}
{"level":"info","ts":"2025-01-14T12:38:25Z","msg":"Next tick at 2.994788 minutes, for label minikube.k8s.io/primary=true","controller":"nodenotifier","controllerGroup":"mr.dopey","controllerKind":"NodeNotifier","NodeNotifier":{"name":"primary"},"namespace":"","name":"primary","reconcileID":"5eb0d366-044d-4284-860a-290917218f98","nodenotifier":{"name":"primary"}}
{"level":"info","ts":"2025-01-14T12:38:25Z","msg":"Slack request made with status code 200","controller":"nodenotifier","controllerGroup":"mr.dopey","controllerKind":"NodeNotifier","NodeNotifier":{"name":"primary"},"namespace":"","name":"primary","reconcileID":"5eb0d366-044d-4284-860a-290917218f98","nodenotifier":{"name":"primary"}}
{"level":"info","ts":"2025-01-14T12:38:51Z","logger":"setup","msg":"Next tick at 2.984441 minutes, for label minikube.k8s.io/primary=false"}
{"level":"info","ts":"2025-01-14T12:38:52Z","logger":"setup","msg":"Slack request made with status code 200"}
{"level":"info","ts":"2025-01-14T12:39:28Z","logger":"setup","msg":"Node minikube-m02 watch event MODIFIED"}
{"level":"info","ts":"2025-01-14T12:39:28Z","logger":"setup","msg":"Node minikube-m02 watch event MODIFIED"}
{"level":"info","ts":"2025-01-14T12:39:28Z","logger":"setup","msg":"Node minikube-m02 watch event DELETED"}
{"level":"info","ts":"2025-01-14T12:39:28Z","logger":"setup","msg":"Was watching node minikube-m02 for NodeNotifier secondary (label minikube.k8s.io/primary=false), but it's been removed. Recalculating next timer."}
{"level":"info","ts":"2025-01-14T12:39:28Z","logger":"setup","msg":"No nodes found with label minikube.k8s.io/primary=false, stopping the watch"}
```

# Build

# Testing

```shell
kubectl apply -f ./yaml/crd.yaml
# Edit then apply
kubectl apply -f ./yaml/example.yaml
minikube start --feature-gates=WatchList=true

docker build -t nodenotifier-controller .
minikube image load nodenotifier-controller

# When image is built
kubectl apply -f ./yaml/controller.yaml

minikube node add
minikube node delete minikube-m02
```

# Reference

- https://github.com/wardviaene/golang-demos/tree/main/kubernetes-operator
- https://blog.mimacom.com/k8s-watch-resources/
- https://api.slack.com/messaging/webhooks