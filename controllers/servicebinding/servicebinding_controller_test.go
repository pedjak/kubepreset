/*
Copyright 2020 The KubePreset Authors

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

package servicebinding_test

import (
	"context"
	"os"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apixv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	sbv1alpha2 "github.com/kubepreset/kubepreset/apis/servicebinding/v1alpha2"
)

var _ = Describe("ServiceBinding Controller:", func() {

	const (
		timeout       = time.Second * 20
		interval      = time.Millisecond * 250
		testNamespace = "default"
	)

	Context("When creating ServiceBinding with ProvisionedService", func() {

		It("should update the ServiceBinding status conditions for type `Ready` with value `True`", func() {
			ctx := context.Background()
			By("Creating BackingService CRD")
			backingServiceCRD := &apixv1.CustomResourceDefinition{
				ObjectMeta: metav1.ObjectMeta{
					Name: "backingservices.app.example.org",
				},
				Spec: apixv1.CustomResourceDefinitionSpec{
					Group: "app.example.org",
					Versions: []apixv1.CustomResourceDefinitionVersion{{
						Name:    "v1alpha1",
						Served:  true,
						Storage: true,
						Schema: &apixv1.CustomResourceValidation{
							OpenAPIV3Schema: &apixv1.JSONSchemaProps{
								Type: "object",
								Properties: map[string]apixv1.JSONSchemaProps{
									"status": {
										Type: "object",
										Properties: map[string]apixv1.JSONSchemaProps{
											"binding": {
												Type: "object",
												Properties: map[string]apixv1.JSONSchemaProps{
													"name": {
														Type: "string",
													},
												},
												Required: []string{"name"},
											},
										},
									},
								},
							},
						},
					},
					},
					Names: apixv1.CustomResourceDefinitionNames{
						Plural: "backingservices",
						Kind:   "BackingService",
					},
					Scope: apixv1.ClusterScoped,
				}}
			Expect(k8sClient.Create(ctx, backingServiceCRD)).Should(Succeed())

			backingServiceCRDLookupKey := types.NamespacedName{Name: "backingservices.app.example.org"}
			createdBackingServiceCRD := &apixv1.CustomResourceDefinition{}

			By("Verifying BackingService CRD")
			// Retry getting newly created BackingService CRD
			// Important: This is required as it is going to be used immediately
			Eventually(func() bool {
				err := k8sClient.Get(ctx, backingServiceCRDLookupKey, createdBackingServiceCRD)
				if err != nil {
					return false
				}
				for _, condition := range createdBackingServiceCRD.Status.Conditions {
					if condition.Type == apixv1.Established &&
						condition.Status == apixv1.ConditionTrue {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())

			By("Creating Secret")
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "secret1",
					Namespace: testNamespace,
				},
				StringData: map[string]string{
					"type":     "custom",
					"provider": "backingservice",
					"username": "guest",
					"password": "password",
				},
			}
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			By("Creating BackingService CR")
			backingServiceCR := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"kind":       "BackingService",
					"apiVersion": "app.example.org/v1alpha1",
					"metadata": map[string]interface{}{
						"name": "back1",
					},
					"status": map[string]interface{}{
						"binding": map[string]interface{}{
							"name": "secret1",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, backingServiceCR)).Should(Succeed())

			matchLabels := map[string]string{
				"environment": "test",
			}

			app := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "app",
					Labels:    matchLabels,
					Namespace: testNamespace,
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: matchLabels,
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: matchLabels,
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								// Ref. https://github.com/kubepreset/bindingdata
								Image: "quay.io/kubepreset/bindingdata:latest",
								Name:  "bindingdata",
							}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, app)).Should(Succeed())

			sb := &sbv1alpha2.ServiceBinding{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "service.binding/v1alpha2",
					Kind:       "ServiceBinding",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sb",
					Namespace: testNamespace,
				},
				Spec: sbv1alpha2.ServiceBindingSpec{
					Application: &sbv1alpha2.Application{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "app",
					},
					Service: &sbv1alpha2.Service{
						APIVersion: "app.example.org/v1alpha1",
						Kind:       "BackingService",
						Name:       "back1",
					},
					Env: []sbv1alpha2.Environment{
						{Name: "BACKING_SERVICE_USERNAME", Key: "username"},
						{Name: "BACKING_SERVICE_PASSWORD", Key: "password"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, sb)).Should(Succeed())

			serviceBindingLookupKey := types.NamespacedName{Name: "sb", Namespace: testNamespace}
			createdServiceBinding := &sbv1alpha2.ServiceBinding{}

			// Retry getting newly created ServiceBinding; the status may not be immediately reflected.
			Eventually(func() bool {
				err := k8sClient.Get(ctx, serviceBindingLookupKey, createdServiceBinding)
				if err != nil {
					return false
				}
				for _, condition := range createdServiceBinding.Status.Conditions {
					if condition.Type == sbv1alpha2.ConditionReady &&
						condition.Status == sbv1alpha2.ConditionTrue {
						return true
					}
				}
				return false

			}, timeout, interval).Should(BeTrue())

			Expect(len(createdServiceBinding.Status.Conditions)).To(Equal(1))
			// FIXME: Fragile?
			Expect(createdServiceBinding.Status.ObservedGeneration).To(Equal(int64(1)))
			Expect(createdServiceBinding.Status.Binding.Name).To(Equal("sb"))

			applicationLookupKey := types.NamespacedName{Name: sb.Spec.Application.Name, Namespace: testNamespace}

			Expect(k8sClient.Get(ctx, applicationLookupKey, app)).Should(Succeed())
			Expect(len(app.Spec.Template.Spec.Volumes)).To(Equal(1))
			Expect(app.Spec.Template.Spec.Volumes[0].Name).To(HavePrefix("sb-"))
			Expect(app.Spec.Template.Spec.Volumes[0].VolumeSource.Secret.SecretName).To(Equal("sb"))
			Expect(app.Spec.Template.Spec.Containers[0].Env).Should(ContainElement(corev1.EnvVar{Name: "BACKING_SERVICE_USERNAME", Value: "guest"}))
			Expect(app.Spec.Template.Spec.Containers[0].Env).Should(ContainElement(corev1.EnvVar{Name: "BACKING_SERVICE_PASSWORD", Value: "password"}))
			Expect(app.Spec.Template.Spec.Containers[0].Env).Should(ContainElement(corev1.EnvVar{Name: "SERVICE_BINDING_ROOT", Value: "/bindings"}))
			Expect(app.Spec.Template.Spec.Containers[0].VolumeMounts[0].Name).To(HavePrefix("sb-"))
			Expect(app.Spec.Template.Spec.Containers[0].VolumeMounts[0].MountPath).To(Equal("/bindings/sb"))

			// FIXME: This seems to be only working an existing cluster
			if os.Getenv("USE_EXISTING_CLUSTER") != "" {
				podTimeout := time.Minute * 3
				podInterval := time.Second * 20
				podList := &corev1.PodList{}
				Eventually(func() bool {
					err := k8sClient.List(ctx, podList, client.InNamespace(testNamespace), client.MatchingLabels{"environment": "test"})
					if err != nil {
						return false
					}
					if len(podList.Items) > 0 {
						ready := []bool{}
						for _, p := range podList.Items {
							found := false
							for _, condition := range p.Status.Conditions {
								if condition.Type == corev1.PodReady &&
									condition.Status == corev1.ConditionTrue {
									ready = append(ready, true)
									found = true
									break
								}
							}
							if !found {
								ready = append(ready, false)
							}
						}
						for _, v := range ready {
							if v == false {
								return false
							}
						}
						return true
					}
					return false
				}, podTimeout, podInterval).Should(BeTrue())

				podList2 := &corev1.PodList{}
				Eventually(func() bool {
					err := k8sClient.List(ctx, podList2, client.InNamespace(testNamespace), client.MatchingLabels{"environment": "test"})
					return err == nil
				}, podTimeout, podInterval).Should(BeTrue())

				Expect(podList2.Items[0].Spec.Containers[0].Env).Should(ContainElement(corev1.EnvVar{Name: "SERVICE_BINDING_ROOT", Value: "/bindings"}))
				found := false
				for _, vm := range podList2.Items[0].Spec.Containers[0].VolumeMounts {
					if vm.MountPath == "/bindings/sb" {
						found = true
						Expect(vm.Name).To(HavePrefix("sb-"))
						Expect(vm.ReadOnly).To(Equal(true))
					}
				}
				Expect(found).To(Equal(true))
			}
		})
	})
})
