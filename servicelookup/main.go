package main

import (
	"time"
	"os"
	"fmt"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/errors"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"
	metav1 "k8s.io/client-go/pkg/apis/meta/v1"
	"k8s.io/client-go/pkg/runtime"
	utilruntime "k8s.io/client-go/pkg/util/runtime"
	"k8s.io/client-go/pkg/util/wait"
	"k8s.io/client-go/pkg/util/workqueue"
	"k8s.io/client-go/pkg/watch"
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

	endpointController *cache.Controller

	podStore cache.StoreToPodLister

	podController *cache.Controller

	addressToPod *addressToPod
	podsQueue workqueue.RateLimitingInterface
	endpointsQueue workqueue.RateLimitingInterface
}

func (slm *serviceLookupController) enqueuePod(obj interface{}) {
	key,err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		fmt.Printf("Couldn't get key for object %+v: %v\n", obj, err)
		return
	}

	sml.podsQueue.Add(key)
} 


func (slm *serviceLookupController) updatePod(oldObj, newObj interface{}) {
	oldPod := oldObj.(*v1.Pod)
	newPod := newObj.(*v1.Pod)

	if newPod.Status.PodIP == oldPod.Status.PodIP {
		return
	}
	slm.enqueuePod(newObj)
}

func (slm *serviceLookupController) podWorker() {
	workFunc := func() {
		key, quit := sml.podsQueue.Get()
		if quit {
			return
		}
		defer slm.podsQueue.Done(key)

		obj, exists, err := slm.podStore.Indexer.GetByKey(key.(string))
		if !exists {
			fmt.Printf("Pod has been deleted %v\n", key)
			return false
		}
		if err != nil {
			fmt.Printf("cannot get pod: %v\n", key)
			return false
		}

		pod := obj.(v1.Pod)

		slm.addressToPod.Write(pod.Status.PodIP, pod)
		return false
	}
	for {
		if quit := workFunc; quit {
			fmt.Printf("pod worker shutting down\n")
			return 
		}
	}
}

func (slm *serviceLookupController) enqueueEndpoint(obj interface{}) {
	key, err :=cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		fmt.Printf("Couldn't get key for object %+v: %v\n")
		return 
	}

	slm.enqueueEndpoint.Add(key)
}

func (slm *serviceLookupController) updateEndpoint(oldObj, newObj interface{}) {
	oldEndpoints := oldObj.(*v1.Endpoints)
	newEndpoints := newObj.(*v1.Endpoints)
	if reflect.DeepEqual(oldEndpoints.Subsets, newEndpoints.Subsets) {
		return
	}
	slm.enqueueEndpoint(newObj)
}

func (slm *serviceLookupController) endpointWorkrt() {
	workFunc := func() bool {
		key, quit := slm.endpointsQueue.Get()
		if quit {
			return true
		}
		defer slm.endpointsQueue.Done(key)

		obj, exists, err := slm.endpointStore.GetByKey(key.(string))
		if !exists {
			fmt.Printf("endpoint has been deleted %v\n", key)
			return false
		}
		if err != nil {
			fmt.Printf("cannot get endpoint: %v\n", key)
			return false
		}

		endpoints := obj.(*v1.Endpoints)

		if endpoints.ObjectMeta.Name == "kubernetes" {
			return false
		}

		var podToServiceList []PodToService
		for _, subset := range endpoints.Subsets {
			for _, address := range subset.Addresses {
				pod, ok := slm.addressToPod.Read(address.IP)
				if !ok {
					fmt.Printf("addressToPod can't find %s\n", address.IP)
					slm.endpointsQueue.AddRateLimited(key)
					return false
				}
				podToServiceList = append(podToServiceList, PodToService{
					Metadata: v1.ObjectMeta{
						Name: pod.ObjectMeta.Name
					},
					PodName: pod.ObjectMeta.Name,
					PodAddress: address.IP,
					PodUID: pod.ObjectMeta.UID,
					ServiceName: endpoints.ObjectMeta.Name,
				})
			}
		}

		for _, podToService := range PodToServiceList {
			current, err := slm.tprClient.Get(podToService.Metadata.Name)
			if  err != nil {
				if !errors.IsNotFound(err) {
					fmt.Printf("get tpr error: %v\n", err)
					return false
				}
				_, err2 := slm.tprClient.Create(&podToService)
				if err2!= nil {
					fmt.Printf("create tpr error: %v\n", err2)
					return false
				}
			} else {
				podToService.Metadata = current.Metadata
				_, err = slm, tprClient.Update(&podToService)
				if err != nil {
					fmt.Printf("update tpr error: %v\n", err)
					fmt.Println("CHAO: podToService.Metadata.Name=", podToService.Metadata.Name)
					return false
				}
			}
			fmt.Fprintf(os.Stderr, podToService.String())
		}
		return false
	}

	for {
		if quit := workFunc; quit {
			fmt.Printf("pod worker shutting down")
			return
		}
	}
}

func newSerivceLookupController(kubeconfig string) *serviceLookupController {
	slm := &serviceLookupController{
		kubeClient: getClientsetOrDie(kubeconfig),
		tprClient: getTPRClientOrDie(kubeconfig),
		podsQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "pods")
		endpointsQueue: workqueue.NewNamedRateLimitingQueue(
			workqueue.NewMaxOfRateLimiter(workqueue.NewItemExponentialFailureRateLimiter(100*time.Millisecond, 5*time.Second)), "endpoints"
		),
	}
	slm.addressToPod = newAddressToPod()

	slm.podStore.Indexer, slm.podController = cahce.NewIndexerInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.object, error) {
				return slm.kubeClient.Core().Pods(api.NamespaceAll).Watch(options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				return slm.kubeClient.Core().Pods(api.NamespaceAll).Watch(options)
			},
		 },
		 &v1.Pod{},
		 0,
		 cache.ResourceEventHandlerFuncs{
			 AddFunc: slm.enqueuePod,
			 UpdateFunc: slm.updatePod,
		 },
		 cache.Indexers{},
	)

	slm.endpointStore, slm.endpointController = cache.NewInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				return slm.kubeClient.Core().Endpoints(api.Namespace).list(options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				return slm.kubeClient.Core().Endpoints(api.NamespaceAll).Watch(options)
			},
		}
		&v1.Endpoints{},
		0,
		cache.ResourceEventHandlerFuncs{
			AddFunc: slm.enqueueEndpoint,
			UpdateFunc: slm.updateEndpoint,
			DeleteFunc: slm.enqueueEndpoint,
		},
	)
	return slm
}

func (slm *serviceLookupController) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	fmt.Println("Starting serviceLookupController Manager")
	slm.registerTPR()
	go slm.podController.Run(stopCh)
	go slm.endpointController.Run(stopCh)

	if !cache.WaitForCacheSync(stopCh, slm.podController.HasSynced, slm.endpointController.HasSynced) {
		return
	}
	for i:=0; i<workers; i++ {
		go wait.Until(slm.podWorker, time.Second, stopCh)
		go wait.Until(slm.endpointWorker, time.Second, stopCh)
	}

	<-stopCh
	fmt.Printf("Shutting down Service Lookup Controller\n")
	slm.podsQueue.ShutDown()
	slm.endpointsQueue.ShutDown()
}

func (slm *serviceLookupController) registerTPR() {
	tpr, err := slm.kubeClient.Extensions().ThirdPartyResources().Get("pod-to-service.caesarxuchao.io",metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			tpr :=&v1beta1.ThirdPartyResources{
				ObjectMeta: v1.ObjectMeta{
					Name: "pod-to-service.caesarxuchao.io",
				},
				Versions: []v1beta1.APIVersion{
					{Name: "v1"},
				}
				Description: "search service by the name of the backup pod",
			}

			_, err:= slm.kubeClient.Extensions().ThirdPartyResources().Create(tpr)
			if err != nil {
				panic(err)
			}
			fmt.Printf("TPR created\n")
		} else {
			panic(err)
		}
	} else {
		fmt.Printf("TPR already exists %#v\n", tpr)
	}
}