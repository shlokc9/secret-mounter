package custom

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	appsinformers "k8s.io/client-go/informers/apps/v1"
	"k8s.io/client-go/kubernetes"
	appslisters "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

type controller struct {
	clientSet      kubernetes.Interface
	depLister      appslisters.DeploymentLister
	depCacheSynced cache.InformerSynced
	workQueue      workqueue.RateLimitingInterface
}

func (cntrlreceiver *controller) handleAdd(obj interface{}) {
	cntrlreceiver.workQueue.Add(obj)
}

func (cntrlreceiver *controller) Run(stopCh <-chan struct{}) {
	fmt.Println("Starting Custom Controller....")
	if !cache.WaitForCacheSync(stopCh, cntrlreceiver.depCacheSynced) {
		fmt.Println("Waiting for the cache to be synced....")
	}

	go wait.Until(cntrlreceiver.worker, 1*time.Second, stopCh)

	<-stopCh
}

func (cntrlreceiver *controller) worker() {
	for cntrlreceiver.processItem() {
	}
}

func (cntrlreceiver *controller) processItem() bool {
	item, shutdown := cntrlreceiver.workQueue.Get()
	if shutdown {
		return false
	}

	defer cntrlreceiver.workQueue.Forget(item)

	key, err := cache.MetaNamespaceKeyFunc(item)
	if err != nil {
		fmt.Println("Error in cache.MetaNamespaceKeyFunc(): ", err.Error())
	}

	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		fmt.Println("Error in cache.SplitMetaNamespaceKey(): ", err.Error())
	}

	err = cntrlreceiver.createSecret(ns, name)
	if err != nil {
		fmt.Println("Error in cntrlreceiver.createSecrets(): ", err.Error())
		return false
	}
	return true
}

func (cntrlreceiver *controller) createSecret(ns, name string) error {
	ctx := context.Background()

	deployment, err := cntrlreceiver.depLister.Deployments(ns).Get(name)
	if err != nil {
		fmt.Println("Error in cntrlreceiver.depLister.Deployments(ns).Get(): ", err.Error())
	}

	if val, ok := deployment.Labels["app"]; ok && val == "secret-sneaker" {

		// Reading secrets from a secret file which will be mounted with the custom controller
		secretFile := "/mnt/data/secret-sneaker-data/secrets.json"
		byteValue, err := ioutil.ReadFile(secretFile)
		if err != nil {
			fmt.Println("Error in ioutil.ReadFile(): ", err.Error())
			fmt.Println("Safely exiting the Secret creation....")
			return nil
		}
		var secrets map[string]string
		err = json.Unmarshal(byteValue, &secrets)
		if err != nil {
			fmt.Println("Error in json.Unmarshal(): ", err.Error())
			fmt.Println("Safely exiting the Secret creation....")
			return nil
		}
		// Declaring the secret resource
		var secretType corev1.SecretType = "Opaque"
		secret := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name + "-secret",
				Namespace: ns,
				Labels:    map[string]string{"app": "secret-sneaker"},
			},
			Type:       secretType,
			StringData: secrets,
		}
		// Creating the secret
		_, err = cntrlreceiver.clientSet.CoreV1().Secrets(ns).Create(ctx, &secret, metav1.CreateOptions{})
		if err != nil {
			cntrlreceiver.clientSet.CoreV1().Secrets(ns).Update(ctx, &secret, metav1.UpdateOptions{})
		}
		fmt.Printf("Secret %s has been created using JSON file on path %s\n", name+"-secret", secretFile)

		// Mounting the secret as a volume in deployment
		err = cntrlreceiver.mountSecretInDeployment(ns, name, ctx)
		if err != nil {
			fmt.Println("Error in cntrlreceiver.mountSecretInDeployment(): ", err.Error())
			return err
		}
	}
	return nil
}

func (cntrlreceiver *controller) mountSecretInDeployment(ns, name string, ctx context.Context) error {
	deployment, err := cntrlreceiver.clientSet.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		fmt.Println("Error in cntrlreceiver.clientSet.AppsV1().Deployments(ns).Get(): ", err.Error())
	}
	// Appending the secret as a volumemount within all the containers in the PodSpec of Deployment
	secretvolume := corev1.Volume{
		Name: name + "-secret-volume",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: name + "-secret",
			},
		},
	}
	deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, secretvolume)
	containervolumemount := corev1.VolumeMount{
		Name:      name + "-secret-volume",
		MountPath: "/etc/secret-sneaker-data/",
		ReadOnly:  true,
	}
	for i := range deployment.Spec.Template.Spec.Containers {
		deployment.Spec.Template.Spec.Containers[i].VolumeMounts = append(deployment.Spec.Template.Spec.Containers[i].VolumeMounts, containervolumemount)
	}
	deployment.ObjectMeta = metav1.ObjectMeta{
		Name: name,
		Namespace: ns,
		Labels: map[string]string{"app": "secret-sneaker"},
	}

	// Updating the deployment so that the new secret is present in the newest pods
	_, err = cntrlreceiver.clientSet.AppsV1().Deployments(ns).Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		fmt.Println("Error in cntrlreceiver.clientSet.AppsV1().Deployments(ns).Update(): ", err.Error())
	}
	fmt.Printf("Deployment %s has been updated with the secret file as a VolumeMount\n", name)
	return nil
}

func InitController(clientSet kubernetes.Interface, depInformer appsinformers.DeploymentInformer) *controller {
	newcontroller := &controller{
		clientSet:      clientSet,
		depLister:      depInformer.Lister(),
		depCacheSynced: depInformer.Informer().HasSynced,
		workQueue:      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "secret-sneaker"),
	}
	depInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: newcontroller.handleAdd,
		},
	)
	return newcontroller
}
