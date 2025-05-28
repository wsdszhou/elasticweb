/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"

	elasticwebv1 "elasticweb/api/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	APP_NAME       = "elastic-app"
	CONTAINER_PORT = 8080
	CPU_REQUEST    = "100m"
	MEM_REQUEST    = "512Mi"
)

// ElasticWebReconciler reconciles a ElasticWeb object
type ElasticWebReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

func getExpectReplicas(elasticWeb *elasticwebv1.ElasticWeb) int32 {
	singlePodQPS := *(elasticWeb.Spec.SinglePodQPS)

	totalQPS := *(elasticWeb.Spec.TotalQPS)

	replicas := totalQPS / singlePodQPS

	if totalQPS%singlePodQPS > 0 {
		replicas++
	}

	return replicas
}

func createServiceIfNotExists(ctx context.Context, r *ElasticWebReconciler, elasticWeb *elasticwebv1.ElasticWeb, req ctrl.Request) error {
	log := ctrl.Log.WithName("createServiceIfNotExists")

	service := &corev1.Service{}

	err := r.Get(ctx, req.NamespacedName, service)

	if !errors.IsNotFound(err) {
		log.Error(err, "Query service error")
		return err
	}

	if err != nil {
		log.Info("Creating a new service", "Service.Namespace", req.Namespace, "Service.Name", req.Name)
		service = &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      req.Name,
				Namespace: req.Namespace,
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Name:     "http",
						Port:     CONTAINER_PORT,
						NodePort: *elasticWeb.Spec.Port,
					},
				},
				Selector: map[string]string{
					"app": APP_NAME,
				},
				Type: corev1.ServiceTypeNodePort,
			},
		}
		log.Info(" set referenced service")
		if err := ctrl.SetControllerReference(elasticWeb, service, r.Scheme); err != nil {
			log.Error(err, "Set controller reference error")
			return err
		}

		log.Info(" start create service")
		if err := r.Create(ctx, service); err != nil {
			log.Error(err, " Create service error")
			return err
		}
		log.Info("create service success")
	}
	return nil
}

func createDeploymentIfNotExists(ctx context.Context, r *ElasticWebReconciler, elasticWeb *elasticwebv1.ElasticWeb, req ctrl.Request) error {
	log := ctrl.Log.WithName("createDeploymentIfNotExists")

	expectReplicas := getExpectReplicas(elasticWeb)

	log.Info(fmt.Sprintf("expect replicas: %d", expectReplicas))

	deployment := &appsv1.Deployment{}

	err := r.Get(ctx, req.NamespacedName, deployment)
	if !errors.IsNotFound(err) {
		log.Error(err, "query deployment failed")
		return err
	} else {
		log.Info("Creating a new deployment", "Deployment.Namespace", req.Namespace, "Deployment.Name", req.Name)

		deployment = &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      req.Name,
				Namespace: req.Namespace,
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: &expectReplicas,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": APP_NAME,
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": APP_NAME,
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: APP_NAME,
								Ports: []corev1.ContainerPort{
									{
										Name:          "http",
										ContainerPort: CONTAINER_PORT,
										Protocol:      corev1.ProtocolSCTP,
									},
								},
								Image:           elasticWeb.Spec.Image,
								ImagePullPolicy: corev1.PullIfNotPresent,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse(CPU_REQUEST),
										corev1.ResourceMemory: resource.MustParse(MEM_REQUEST),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse(CPU_REQUEST),
										corev1.ResourceMemory: resource.MustParse(MEM_REQUEST),
									},
								},
								Command: []string{"/bin/sh", "-c", "while true; do echo 'hello world'; sleep 1; done"},
							},
						},
					},
				},
			},
		}

		log.Info("set referenced deployment")

		if err := ctrl.SetControllerReference(elasticWeb, deployment, r.Scheme); err != nil {
			log.Error(err, "Set controller reference error")
			return err
		}

		log.Info("start create deployment")
		if err := r.Create(ctx, deployment); err != nil {
			log.Error(err, "Create deployment error")
			return err
		}
		log.Info("create deployment success")
	}
	return nil
}

func updateStatus(ctx context.Context, r *ElasticWebReconciler, elasticWeb *elasticwebv1.ElasticWeb, req ctrl.Request) error {
	log := ctrl.Log.WithName("updateStatus")

	log.Info("start update status")

	singlePodQPS := *(elasticWeb.Spec.SinglePodQPS)

	replicas := getExpectReplicas(elasticWeb)

	if nil == elasticWeb.Status.RealQPS {
		elasticWeb.Status.RealQPS = new(int32)
	}

	*elasticWeb.Status.RealQPS = singlePodQPS * replicas

	log.Info(fmt.Sprintf(fmt.Sprintf("real qps: %d", *elasticWeb.Status.RealQPS)))

	if err := r.Status().Update(ctx, elasticWeb); err != nil {
		log.Error(err, "Update status error")
		return err
	}
	log.Info("update status success")
	return nil

}

// +kubebuilder:rbac:groups=elasticweb.com.jwzhou,resources=elasticwebs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=elasticweb.com.jwzhou,resources=elasticwebs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=elasticweb.com.jwzhou,resources=elasticwebs/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ElasticWeb object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/reconcile
func (r *ElasticWebReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.Log.WithName("Reconcile")

	log.Info("start reconcile")

	instance := &elasticwebv1.ElasticWeb{}

	err := r.Get(ctx, req.NamespacedName, instance)

	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, err
		}

		log.Error(err, "Get elasticWeb error")
		return ctrl.Result{}, err
	}

	log.Info(fmt.Sprintf("elasticWeb: %v", instance))

	deployment := &appsv1.Deployment{}

	err = r.Get(ctx, req.NamespacedName, deployment)

	if err != nil {
		if errors.IsNotFound(err) {

			// 如果对QPS没有需求，此时又没有deployment，就啥事都不做了
			if *(instance.Spec.TotalQPS) < 1 {
				log.Info("not need deployment")
				// 返回
				return ctrl.Result{}, nil
			}

			if err := createServiceIfNotExists(ctx, r, instance, req); err != nil {
				log.Error(err, "Create service error")
				return ctrl.Result{}, err
			}

			if err := createDeploymentIfNotExists(ctx, r, instance, req); err != nil {
				log.Error(err, "Create deployment error")
				return ctrl.Result{}, err
			}

			if err := updateStatus(ctx, r, instance, req); err != nil {
				log.Error(err, "Update status error")
				return ctrl.Result{}, err
			}
		}
	} else {
		expectReplicas := getExpectReplicas(instance)

		realReplicas := *deployment.Spec.Replicas

		if expectReplicas == realReplicas {
			return ctrl.Result{}, err
		}

		*deployment.Spec.Replicas = expectReplicas

		if err := r.Update(ctx, deployment); err != nil {
			log.Error(err, "Update deployment error")
			return ctrl.Result{}, err
		}

		if err := updateStatus(ctx, r, instance, req); err != nil {
			log.Error(err, "Update status error")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ElasticWebReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&elasticwebv1.ElasticWeb{}).
		Named("elasticweb").
		Complete(r)
}
