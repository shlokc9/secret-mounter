package custom

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	appsInformers "k8s.io/client-go/informers/apps/v1"
	"k8s.io/client-go/kubernetes"
	appsListers "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const (
	SecretMounter              string = "secret-mounter"
	DefaultContainerSecretPath string = "/etc/" + SecretMounter + "-data/"
	DeploymentLabelSecretName  string = "secret-name"
	DeploymentLabelSecretKeys  string = "secret-keys"
	SecretKeysSeparator        string = "."
)

type controller struct {
	clientSet      kubernetes.Interface
	depLister      appsListers.DeploymentLister
	depCacheSynced cache.InformerSynced
	workQueue      workqueue.RateLimitingInterface
}

// Initializes custom controller.
func InitController(clientSet kubernetes.Interface, depInformer appsInformers.DeploymentInformer) *controller {
	newController := &controller{
		clientSet:      clientSet,
		depLister:      depInformer.Lister(),
		depCacheSynced: depInformer.Informer().HasSynced,
		workQueue:      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), SecretMounter),
	}
	depInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    newController.handleAdd,
			UpdateFunc: newController.handleUpdate,
		},
	)
	return newController
}

// Handles Add Event on a Deployment.
func (cntrlReceiver *controller) handleAdd(obj interface{}) {
	cntrlReceiver.workQueue.Add(obj)
}

// Handles Update Event on a Deployment.
func (cntrlReceiver *controller) handleUpdate(oldObj interface{}, newObj interface{}) {
	cntrlReceiver.workQueue.Add(newObj)
}

// Runs the custom controller.
func (cntrlReceiver *controller) Run(stopCh <-chan struct{}) {
	fmt.Println("Starting Custom Controller....")
	if !cache.WaitForCacheSync(stopCh, cntrlReceiver.depCacheSynced) {
		fmt.Println("Waiting for the cache to be synced....")
	}
	go wait.Until(cntrlReceiver.worker, 1*time.Second, stopCh)
	<-stopCh
}

// Daemon job that gets the resource item from queue.
// And performs necessary actions on it.
func (cntrlReceiver *controller) worker() {
	for {
		item, shutdown := cntrlReceiver.workQueue.Get()
		if shutdown {
			break
		}
		cntrlReceiver.processItem(item)
	}
}

// Processes the item by fetching it's name and namespace.
func (cntrlReceiver *controller) processItem(item interface{}) bool {
	defer cntrlReceiver.workQueue.Forget(item)
	key, err := cache.MetaNamespaceKeyFunc(item)
	if err != nil {
		fmt.Println("Error in cache.MetaNamespaceKeyFunc(): ", err.Error())
		return false
	}
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		fmt.Println("Error in cache.SplitMetaNamespaceKey(): ", err.Error())
		return false
	}
	err = cntrlReceiver.checkDeployments(ns, name)
	if err != nil {
		fmt.Println("Error in cntrlReceiver.checkDeployments(): ", err.Error())
		return false
	}
	return true
}

// Fetches the deployment using name & namespace.
// And checks for particular label secret-name in the deployment.
func (cntrlReceiver *controller) checkDeployments(ns, name string) error {
	ctx := context.Background()
	deployment, err := cntrlReceiver.depLister.Deployments(ns).Get(name)
	if err != nil {
		fmt.Println("Error in cntrlReceiver.depLister.Deployments(ns).Get(): ", err.Error())
		return err
	}
	depLabels := deployment.Labels
	if secretName, secretNameFilterOk := depLabels[DeploymentLabelSecretName]; secretNameFilterOk {
		secret, err := cntrlReceiver.fetchSecret(ctx, ns, secretName)
		if err != nil {
			fmt.Println("Error in cntrlReceiver.fetchSecret(): ", err.Error())
			return err
		}
		err = cntrlReceiver.updateDeploymentWithSecret(ctx, ns, name, deployment, secret, depLabels)
		if err != nil {
			fmt.Println("Error in cntrlReceiver.updateDeployment(): ", err.Error())
			return err
		}
	}
	return nil
}

// Fetches the secret resource using secret-name in the deployment labels.
func (cntrlReceiver *controller) fetchSecret(ctx context.Context, ns, secretName string) (coreV1.Secret, error) {
	secret, err := cntrlReceiver.clientSet.CoreV1().Secrets(ns).Get(ctx, secretName, metaV1.GetOptions{})
	if err != nil {
		fmt.Println("Error in cntrlReceiver.clientSet.CoreV1().Secrets(ns).Get(): ", err.Error())
		return coreV1.Secret{}, err
	}
	return *secret, nil
}

// Updates the deployment with secret as a volume.
func (cntrlReceiver *controller) updateDeploymentWithSecret(
	ctx context.Context, ns, name string, deployment *appsV1.Deployment, secret coreV1.Secret, depLabels map[string]string) error {
	deployment.ObjectMeta = metaV1.ObjectMeta{
		Name:      name,
		Namespace: ns,
	}
	secretVolume := createSecretVolume(secret)
	secretVolume = addSecretKeysToVolume(depLabels, secret, secretVolume)
	deployment = appendSecretAsVolume(deployment, secret, secretVolume)

	_, err := cntrlReceiver.clientSet.AppsV1().Deployments(ns).Update(ctx, deployment, metaV1.UpdateOptions{})
	if err != nil {
		fmt.Println("Error in cntrlReceiver.clientSet.AppsV1().Deployments(ns).Update(): ", err.Error())
		return err
	}
	fmt.Printf("Deployment %s has been updated with desired keys in secret %s\n", name, secret.Name)
	return nil
}

// Creates a volume resource of source type Secret.
func createSecretVolume(secret coreV1.Secret) coreV1.Volume {
	return coreV1.Volume{
		Name: secret.Name + "-secret-volume",
		VolumeSource: coreV1.VolumeSource{
			Secret: &coreV1.SecretVolumeSource{
				SecretName: secret.Name,
			},
		},
	}
}

// Fetches the secret-keys from the deployment labels and adds them as items to volume of type secret.
func addSecretKeysToVolume(depLabels map[string]string, secret coreV1.Secret, secretVolume coreV1.Volume) coreV1.Volume {
	if secretKeys, secretKeysOk := depLabels[DeploymentLabelSecretKeys]; secretKeysOk {
		secretKeysList := strings.Split(secretKeys, SecretKeysSeparator)
		for _, key := range secretKeysList {
			_, keyCheckDataOk := secret.Data[key]
			_, keyCheckStringDataOk := secret.StringData[key]
			if !(keyCheckDataOk || keyCheckStringDataOk) {
				fmt.Printf("Key %s not found in the mentioned secret %s - Skipping mount for key %s\n", key, secret.Name, key)
				continue
			}
			secretVolume.VolumeSource.Secret.Items = append(secretVolume.VolumeSource.Secret.Items, coreV1.KeyToPath{
				Key:  key,
				Path: key,
			})
		}
	}
	return secretVolume
}

// Appends the secret as a volume to the deployment.
func appendSecretAsVolume(deployment *appsV1.Deployment, secret coreV1.Secret, secretVolume coreV1.Volume) *appsV1.Deployment {
	deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, secretVolume)
	deployment = appendContainerVolumeMount(deployment, secret)
	return deployment
}

// Appends the volume mounts to each container spec.
func appendContainerVolumeMount(deployment *appsV1.Deployment, secret coreV1.Secret) *appsV1.Deployment {
	for i := range deployment.Spec.Template.Spec.Containers {
		deployment.Spec.Template.Spec.Containers[i].VolumeMounts = append(
			deployment.Spec.Template.Spec.Containers[i].VolumeMounts, coreV1.VolumeMount{
				Name:      secret.Name + "-secret-volume",
				MountPath: DefaultContainerSecretPath,
				ReadOnly:  true,
			})
	}
	return deployment
}
