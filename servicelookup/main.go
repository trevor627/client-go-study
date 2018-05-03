package main

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

func getClientsetOrDie(kubeconfig string) *kubernetes.Clientset {
	config, err : clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		panic(err)
	}

	clientset, err:= kubernetes.NewForConfig(config) 
	if err !=nil {
		panic(err)
	}
	return clientset
}

func main() {
	kubeconfig := flag.String("kubeconfig", "", "Path to a kube config. Only required if out-of-cluster.")
	flag.Parse()
	controller := newServiceLookupController(*kubeconfig)
	var stopCh <-chan struct{}
	controller.Run(2, stopCh)
}

type serviceLookupController struct {
	kubeClient *kubernetes.Clientset
	tprClient *podToServiceClient

	endpointStore cache.store
}