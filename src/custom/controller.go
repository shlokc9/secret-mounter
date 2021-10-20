package custom

import (
	"context"
	"fmt"
	"strings"
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

const SecretMounter string = "secret-mounter"
const DefaultContainerSecretPath string = "/etc/" + SecretMounter + "-data/"

// Mandatory field
const DeploymentLabelSecretName string = "secret-name"
// Optional field
const DeploymentLabelSecretKeys string = "secret-keys"
// Secret Keys separator
const SecretKeysSeparator string = "."

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
	err = cntrlReceiver.checkDeployments(ns, name)
	if err != nil {
		fmt.Println("Error in cntrlReceiver.checkDeployments(): ", err.Error())
		return false
	}
	return true
}

func (cntrlReceiver *controller) checkDeployments(ns, name string) error {
	// Get Deployment from api-server
	ctx := context.Background()
	deployment, err := cntrlReceiver.depLister.Deployments(ns).Get(name)
	if err != nil {
		fmt.Println("Error in cntrlReceiver.depLister.Deployments(ns).Get(): ", err.Error())
	}
	// Check the deployment metadata.labels
	depLabels := deployment.Labels
	if labelFilter, labelFilterOk := depLabels["app"]; labelFilterOk && labelFilter == SecretMounter {
		if secretName, secretNameFilterOk := depLabels[DeploymentLabelSecretName]; secretNameFilterOk {
			// Get the secret from the api-server
			secret, err := cntrlReceiver.clientSet.CoreV1().Secrets(ns).Get(ctx, secretName, metaV1.GetOptions{})
			if err != nil {
				fmt.Println("Error in cntrlReceiver.clientSet.CoreV1().Secrets(ns).Get(): ", err.Error())
				return err
			}
			// Mount secret as a volume in deployment
			err = cntrlReceiver.mountSecretInDep(ctx, ns, name, *secret, depLabels)
			if err != nil {
				fmt.Println("Error in cntrlReceiver.mountSecretInDep(): ", err.Error())
				return err
			}
		} else {
			fmt.Println("Secret name not found in the deployment metadata.labels - Skipping secret mount....")
		}
	}
	return nil
}

func (cntrlReceiver *controller) mountSecretInDep(
	// Fetching the latest version of deployment and modifying it
	ctx context.Context, ns, name string, secret coreV1.Secret, depLabels map[string]string) error {
	deployment, err := cntrlReceiver.clientSet.AppsV1().Deployments(ns).Get(ctx, name, metaV1.GetOptions{})
	if err != nil {
		fmt.Println("Error in cntrlReceiver.clientSet.AppsV1().Deployments(ns).Get(): ", err.Error())
	}
	secretVolume := coreV1.Volume{
		Name: secret.Name + "-secret-volume",
		VolumeSource: coreV1.VolumeSource{
			Secret: &coreV1.SecretVolumeSource{
				SecretName: secret.Name,
			},
		},
	}
	containerVolumeMount := coreV1.VolumeMount{
		Name:      secret.Name + "-secret-volume",
		MountPath: DefaultContainerSecretPath,
		ReadOnly:  true,
	}
	deployment.ObjectMeta = metaV1.ObjectMeta{
		Name:        name,
		Namespace:   ns,
	}
	// Get and add secret keys from deployment metadata.labels
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
	// Appending the secret as a volumemount within all the containers in the PodSpec of Deployment
	deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, secretVolume)
	for i := range deployment.Spec.Template.Spec.Containers {
		deployment.Spec.Template.Spec.Containers[i].VolumeMounts = append(
			deployment.Spec.Template.Spec.Containers[i].VolumeMounts, containerVolumeMount)
	}
	// Updating the deployment so that the new secret is present in the newest pods
	_, err = cntrlReceiver.clientSet.AppsV1().Deployments(ns).Update(ctx, deployment, metaV1.UpdateOptions{})
	if err != nil {
		fmt.Println("Error in cntrlReceiver.clientSet.AppsV1().Deployments(ns).Update(): ", err.Error())
		return err
	}
	fmt.Printf("Deployment %s has been updated with desired keys in secret %s\n", name, secret.Name)
	return nil
}

func InitController(clientSet kubernetes.Interface, depInformer appsInformers.DeploymentInformer) *controller {
	newController := &controller{
		clientSet:      clientSet,
		depLister:      depInformer.Lister(),
		depCacheSynced: depInformer.Informer().HasSynced,
		workQueue:      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), SecretMounter),
	}
	depInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: newController.handleAdd,
		},
	)
	return newController
}
