package main

import (
	"os"
	"fmt"
	"time"
	"path/filepath"

	"github.com/shlokchaudhari9/secret-mounter/custom"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	config, err := rest.InClusterConfig()
	if err != nil {
		// fallback to kubeconfig
		kubeconfig := filepath.Join("/home/infracloud", ".kube", "config")
		if envvar := os.Getenv("KUBECONFIG"); len(envvar) >0 {
			kubeconfig = envvar
		}
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			fmt.Println("Error in clientcmd.BuildConfigFromFlags()", err.Error())
			os.Exit(1)
		}
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		fmt.Println("Error in kubernetes.NewForConfig()", err.Error())
	}

	stopCh := make(chan struct{})

	informers := informers.NewSharedInformerFactory(clientset, time.Second*30)

	controller := custom.InitController(clientset, informers.Apps().V1().Deployments())
	informers.Start(stopCh)
	controller.Run(stopCh)
}
