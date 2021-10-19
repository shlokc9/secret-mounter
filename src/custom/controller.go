package custom

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"time"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	appsInformers "k8s.io/client-go/informers/apps/v1"
	"k8s.io/client-go/kubernetes"
	appsListers "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const SecretSneaker string = "secret-sneaker"
const ContainerSecretPath string = "/etc/" + SecretSneaker + "-data/"
const HostSecretMountPath string = "/mnt/data/" + SecretSneaker + "-data/"

type controller struct {
	clientSet      kubernetes.Interface
	depLister      appsListers.DeploymentLister
	depCacheSynced cache.InformerSynced
	workQueue      workqueue.RateLimitingInterface
}

func (cntrlReceiver *controller) handleAdd(obj interface{}) {
	cntrlReceiver.workQueue.Add(obj)
}

func (cntrlReceiver *controller) Run(stopCh <-chan struct{}) {
	fmt.Println("Starting Custom Controller....")
	if !cache.WaitForCacheSync(stopCh, cntrlReceiver.depCacheSynced) {
		fmt.Println("Waiting for the cache to be synced....")
	}
	go wait.Until(cntrlReceiver.worker, 1*time.Second, stopCh)
	<-stopCh
}

func (cntrlReceiver *controller) worker() {
	for cntrlReceiver.processItem() {
	}
}

func (cntrlReceiver *controller) processItem() bool {
	item, shutdown := cntrlReceiver.workQueue.Get()
	if shutdown {
		return false
	}
	defer cntrlReceiver.workQueue.Forget(item)
	key, err := cache.MetaNamespaceKeyFunc(item)
	if err != nil {
		fmt.Println("Error in cache.MetaNamespaceKeyFunc(): ", err.Error())
	}
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		fmt.Println("Error in cache.SplitMetaNamespaceKey(): ", err.Error())
	}
	err = cntrlReceiver.createSecret(ns, name)
	if err != nil {
		fmt.Println("Error in cntrlReceiver.createSecrets(): ", err.Error())
		return false
	}
	return true
}

func (cntrlReceiver *controller) createSecret(ns, name string) error {
	ctx := context.Background()
	deployment, err := cntrlReceiver.depLister.Deployments(ns).Get(name)
	if err != nil {
		fmt.Println("Error in cntrlReceiver.depLister.Deployments(ns).Get(): ", err.Error())
	}
	if val, ok := deployment.Labels["app"]; ok && val == SecretSneaker {
		// Reading secrets from a secret file which will be mounted with the custom controller
		secretFile := HostSecretMountPath + "secrets.json"
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
		var secretType coreV1.SecretType = "Opaque"
		secret := coreV1.Secret{
			ObjectMeta: metaV1.ObjectMeta{
				Name:      name + "-secret",
				Namespace: ns,
				Labels:    map[string]string{"app": SecretSneaker},
			},
			Type:       secretType,
			StringData: secrets,
		}
		// Creating the secret
		_, err = cntrlReceiver.clientSet.CoreV1().Secrets(ns).Create(ctx, &secret, metaV1.CreateOptions{})
		if err != nil {
			cntrlReceiver.clientSet.CoreV1().Secrets(ns).Update(ctx, &secret, metaV1.UpdateOptions{})
		}
		fmt.Printf("Secret %s has been created using JSON file on path %s\n", name+"-secret", secretFile)
		
		// Mounting the secret as a volume in deployment
		err = cntrlReceiver.mountSecretInDeployment(ns, name, ctx)
		if err != nil {
			fmt.Println("Error in cntrlReceiver.mountSecretInDeployment(): ", err.Error())
			return err
		}
	}
	return nil
}

func (cntrlReceiver *controller) mountSecretInDeployment(ns, name string, ctx context.Context) error {
	// Appending the secret as a volumemount within all the containers in the PodSpec of Deployment
	deployment, err := cntrlReceiver.clientSet.AppsV1().Deployments(ns).Get(ctx, name, metaV1.GetOptions{})
	if err != nil {
		fmt.Println("Error in cntrlReceiver.clientSet.AppsV1().Deployments(ns).Get(): ", err.Error())
	}
	secretVolume := coreV1.Volume{
		Name: name + "-secret-volume",
		VolumeSource: coreV1.VolumeSource{
			Secret: &coreV1.SecretVolumeSource{
				SecretName: name + "-secret",
			},
		},
	}
	deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, secretVolume)
	containerVolumeMount := coreV1.VolumeMount{
		Name:      name + "-secret-volume",
		MountPath: ContainerSecretPath,
		ReadOnly:  true,
	}
	for i := range deployment.Spec.Template.Spec.Containers {
		deployment.Spec.Template.Spec.Containers[i].VolumeMounts = append(deployment.Spec.Template.Spec.Containers[i].VolumeMounts, containerVolumeMount)
	}
	deployment.ObjectMeta = metaV1.ObjectMeta{
		Name:      name,
		Namespace: ns,
		Labels:    map[string]string{"app": SecretSneaker},
	}

	// Updating the deployment so that the new secret is present in the newest pods
	_, err = cntrlReceiver.clientSet.AppsV1().Deployments(ns).Update(ctx, deployment, metaV1.UpdateOptions{})
	if err != nil {
		fmt.Println("Error in cntrlReceiver.clientSet.AppsV1().Deployments(ns).Update(): ", err.Error())
	}
	fmt.Printf("Deployment %s has been updated with the secret file as a VolumeMount\n", name)
	return nil
}

func InitController(clientSet kubernetes.Interface, depInformer appsInformers.DeploymentInformer) *controller {
	newController := &controller{
		clientSet:      clientSet,
		depLister:      depInformer.Lister(),
		depCacheSynced: depInformer.Informer().HasSynced,
		workQueue:      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), SecretSneaker),
	}
	depInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: newController.handleAdd,
		},
	)
	return newController
}
