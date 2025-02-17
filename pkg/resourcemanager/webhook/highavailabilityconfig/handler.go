// Copyright (c) 2022 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package highavailabilityconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Masterminds/semver"
	hvpav1alpha1 "github.com/gardener/hvpa-controller/api/v1alpha1"
	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	autoscalingv2beta1 "k8s.io/api/autoscaling/v2beta1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	resourcesv1alpha1 "github.com/gardener/gardener/pkg/apis/resources/v1alpha1"
	kubernetesutils "github.com/gardener/gardener/pkg/utils/kubernetes"
	versionutils "github.com/gardener/gardener/pkg/utils/version"
)

// Handler handles admission requests and sets the following fields based on the failure tolerance type and the
// component type:
// - `.spec.replicas`
// - `.spec.template.spec.affinity`
// - `.spec.template.spec.topologySpreadConstraints`
type Handler struct {
	Logger        logr.Logger
	TargetClient  client.Reader
	TargetVersion *semver.Version

	decoder *admission.Decoder
}

// InjectDecoder injects the decoder.
func (h *Handler) InjectDecoder(d *admission.Decoder) error {
	h.decoder = d
	return nil
}

// Handle defaults the high availability settings of the provided resource.
func (h *Handler) Handle(ctx context.Context, req admission.Request) admission.Response {
	var (
		requestGK = schema.GroupKind{Group: req.Kind.Group, Kind: req.Kind.Kind}
		obj       runtime.Object
		err       error
	)

	namespace := &corev1.Namespace{}
	if err := h.TargetClient.Get(ctx, client.ObjectKey{Name: req.Namespace}, namespace); err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	var (
		failureToleranceType *gardencorev1beta1.FailureToleranceType
		zones                []string
		isZonePinningEnabled bool
	)

	if v, ok := namespace.Annotations[resourcesv1alpha1.HighAvailabilityConfigFailureToleranceType]; ok {
		value := gardencorev1beta1.FailureToleranceType(v)
		failureToleranceType = &value
	}

	if v, ok := namespace.Annotations[resourcesv1alpha1.HighAvailabilityConfigZones]; ok {
		zones = sets.NewString(strings.Split(v, ",")...).Delete("").List()
	}

	if v, err := strconv.ParseBool(namespace.Annotations[resourcesv1alpha1.HighAvailabilityConfigZonePinning]); err == nil {
		isZonePinningEnabled = v
	}

	isHorizontallyScaled, maxReplicas, err := h.isHorizontallyScaled(ctx, req.Namespace, schema.GroupVersion{Group: req.Kind.Group, Version: req.Kind.Version}.String(), req.Kind.Kind, req.Name)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	switch requestGK {
	case appsv1.SchemeGroupVersion.WithKind("Deployment").GroupKind():
		obj, err = h.handleDeployment(req, failureToleranceType, zones, isHorizontallyScaled, maxReplicas, isZonePinningEnabled)
	case appsv1.SchemeGroupVersion.WithKind("StatefulSet").GroupKind():
		obj, err = h.handleStatefulSet(req, failureToleranceType, zones, isHorizontallyScaled, maxReplicas, isZonePinningEnabled)
	case autoscalingv2.SchemeGroupVersion.WithKind("HorizontalPodAutoscaler").GroupKind():
		obj, err = h.handleHorizontalPodAutoscaler(req, failureToleranceType)
	case hvpav1alpha1.SchemeGroupVersionHvpa.WithKind("Hvpa").GroupKind():
		obj, err = h.handleHvpa(req, failureToleranceType)
	default:
		return admission.Allowed(fmt.Sprintf("unexpected resource: %s", requestGK))
	}

	if err != nil {
		var apiStatus apierrors.APIStatus
		if errors.As(err, &apiStatus) {
			result := apiStatus.Status()
			return admission.Response{AdmissionResponse: admissionv1.AdmissionResponse{Allowed: false, Result: &result}}
		}
		return admission.Denied(err.Error())
	}

	marshalled, err := json.Marshal(obj)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshalled)
}

func (h *Handler) handleDeployment(
	req admission.Request,
	failureToleranceType *gardencorev1beta1.FailureToleranceType,
	zones []string,
	isHorizontallyScaled bool,
	maxReplicas int32,
	isZonePinningEnabled bool,
) (
	runtime.Object,
	error,
) {
	deployment := &appsv1.Deployment{}
	if err := h.decoder.Decode(req, deployment); err != nil {
		return nil, err
	}

	log := h.Logger.WithValues("deployment", kubernetesutils.ObjectKeyForCreateWebhooks(deployment, req))

	if err := mutateReplicas(
		log,
		failureToleranceType,
		isHorizontallyScaled,
		deployment,
		deployment.Spec.Replicas,
		func(replicas *int32) { deployment.Spec.Replicas = replicas },
	); err != nil {
		return nil, err
	}

	h.mutateNodeAffinity(
		// TODO(ScheererJ): Remove "failureToleranceType != nil" in the future
		failureToleranceType != nil || isZonePinningEnabled,
		zones,
		&deployment.Spec.Template,
	)

	h.mutateTopologySpreadConstraints(
		failureToleranceType,
		zones,
		isHorizontallyScaled,
		deployment.Spec.Replicas,
		maxReplicas,
		&deployment.Spec.Template,
	)

	return deployment, nil
}

func (h *Handler) handleStatefulSet(
	req admission.Request,
	failureToleranceType *gardencorev1beta1.FailureToleranceType,
	zones []string,
	isHorizontallyScaled bool,
	maxReplicas int32,
	isZonePinningEnabled bool,
) (
	runtime.Object,
	error,
) {
	statefulSet := &appsv1.StatefulSet{}
	if err := h.decoder.Decode(req, statefulSet); err != nil {
		return nil, err
	}

	log := h.Logger.WithValues("statefulSet", kubernetesutils.ObjectKeyForCreateWebhooks(statefulSet, req))

	if err := mutateReplicas(
		log,
		failureToleranceType,
		isHorizontallyScaled,
		statefulSet,
		statefulSet.Spec.Replicas,
		func(replicas *int32) { statefulSet.Spec.Replicas = replicas },
	); err != nil {
		return nil, err
	}

	h.mutateNodeAffinity(
		// TODO(ScheererJ): Remove "failureToleranceType != nil" in the future
		failureToleranceType != nil || isZonePinningEnabled,
		zones,
		&statefulSet.Spec.Template,
	)

	h.mutateTopologySpreadConstraints(
		failureToleranceType,
		zones,
		isHorizontallyScaled,
		statefulSet.Spec.Replicas,
		maxReplicas,
		&statefulSet.Spec.Template,
	)

	return statefulSet, nil
}

func (h *Handler) handleHvpa(req admission.Request, failureToleranceType *gardencorev1beta1.FailureToleranceType) (runtime.Object, error) {
	hvpa := &hvpav1alpha1.Hvpa{}
	if err := h.decoder.Decode(req, hvpa); err != nil {
		return nil, err
	}

	log := h.Logger.WithValues("hvpa", kubernetesutils.ObjectKeyForCreateWebhooks(hvpa, req))

	if err := mutateAutoscalingReplicas(
		log,
		failureToleranceType,
		hvpa,
		func() *int32 { return hvpa.Spec.Hpa.Template.Spec.MinReplicas },
		func(n *int32) { hvpa.Spec.Hpa.Template.Spec.MinReplicas = n },
		func() int32 { return hvpa.Spec.Hpa.Template.Spec.MaxReplicas },
		func(n int32) { hvpa.Spec.Hpa.Template.Spec.MaxReplicas = n },
	); err != nil {
		return nil, err
	}

	return hvpa, nil
}

func (h *Handler) handleHorizontalPodAutoscaler(req admission.Request, failureToleranceType *gardencorev1beta1.FailureToleranceType) (runtime.Object, error) {
	switch req.Kind.Version {
	case autoscalingv2beta1.SchemeGroupVersion.Version:
		hpa := &autoscalingv2beta1.HorizontalPodAutoscaler{}
		if err := h.decoder.Decode(req, hpa); err != nil {
			return nil, err
		}

		log := h.Logger.WithValues("hpa", kubernetesutils.ObjectKeyForCreateWebhooks(hpa, req))

		if err := mutateAutoscalingReplicas(
			log,
			failureToleranceType,
			hpa,
			func() *int32 { return hpa.Spec.MinReplicas },
			func(n *int32) { hpa.Spec.MinReplicas = n },
			func() int32 { return hpa.Spec.MaxReplicas },
			func(n int32) { hpa.Spec.MaxReplicas = n },
		); err != nil {
			return nil, err
		}

		return hpa, nil
	case autoscalingv2.SchemeGroupVersion.Version:
		hpa := &autoscalingv2.HorizontalPodAutoscaler{}
		if err := h.decoder.Decode(req, hpa); err != nil {
			return nil, err
		}

		log := h.Logger.WithValues("hpa", kubernetesutils.ObjectKeyForCreateWebhooks(hpa, req))

		if err := mutateAutoscalingReplicas(
			log,
			failureToleranceType,
			hpa,
			func() *int32 { return hpa.Spec.MinReplicas },
			func(n *int32) { hpa.Spec.MinReplicas = n },
			func() int32 { return hpa.Spec.MaxReplicas },
			func(n int32) { hpa.Spec.MaxReplicas = n },
		); err != nil {
			return nil, err
		}

		return hpa, nil
	default:
		return nil, fmt.Errorf("autoscaling version %q in request is not supported", req.Kind.Version)
	}
}

func mutateReplicas(
	log logr.Logger,
	failureToleranceType *gardencorev1beta1.FailureToleranceType,
	isHorizontallyScaled bool,
	obj client.Object,
	currentReplicas *int32,
	setReplicas func(*int32),
) error {
	replicas, err := getReplicaCount(obj, currentReplicas, failureToleranceType)
	if err != nil {
		return err
	}
	if replicas == nil {
		return nil
	}

	// only mutate replicas if object is not horizontally scaled or if current replica count is lower than what we have
	// computed
	if !isHorizontallyScaled || pointer.Int32Deref(currentReplicas, 0) < *replicas {
		log.Info("Mutating replicas", "replicas", *replicas)
		setReplicas(replicas)
	}

	return nil
}

func getReplicaCount(obj client.Object, currentOrMinReplicas *int32, failureToleranceType *gardencorev1beta1.FailureToleranceType) (*int32, error) {
	// do not mutate replicas if they are set to 0 (hibernation case or HPA disabled)
	if pointer.Int32Deref(currentOrMinReplicas, 0) == 0 {
		return nil, nil
	}

	replicas := kubernetesutils.GetReplicaCount(failureToleranceType, obj.GetLabels()[resourcesv1alpha1.HighAvailabilityConfigType])
	if replicas == nil {
		return nil, nil
	}

	// check if custom replica overwrite is desired
	if replicasOverwrite := obj.GetAnnotations()[resourcesv1alpha1.HighAvailabilityConfigReplicas]; replicasOverwrite != "" {
		v, err := strconv.Atoi(replicasOverwrite)
		if err != nil {
			return nil, err
		}
		replicas = pointer.Int32(int32(v))
	}
	return replicas, nil
}

func (h *Handler) isHorizontallyScaled(ctx context.Context, namespace, targetAPIVersion, targetKind, targetName string) (bool, int32, error) {
	if versionutils.ConstraintK8sGreaterEqual123.Check(h.TargetVersion) {
		hpaList := &autoscalingv2.HorizontalPodAutoscalerList{}
		if err := h.TargetClient.List(ctx, hpaList, client.InNamespace(namespace)); err != nil {
			return false, 0, fmt.Errorf("failed to list all HPAs: %w", err)
		}

		for _, hpa := range hpaList.Items {
			if targetRef := hpa.Spec.ScaleTargetRef; targetRef.APIVersion == targetAPIVersion &&
				targetRef.Kind == targetKind && targetRef.Name == targetName {
				return true, hpa.Spec.MaxReplicas, nil
			}
		}
	} else {
		hpaList := &autoscalingv2beta1.HorizontalPodAutoscalerList{}
		if err := h.TargetClient.List(ctx, hpaList, client.InNamespace(namespace)); err != nil {
			return false, 0, fmt.Errorf("failed to list all HPAs: %w", err)
		}

		for _, hpa := range hpaList.Items {
			if targetRef := hpa.Spec.ScaleTargetRef; targetRef.APIVersion == targetAPIVersion &&
				targetRef.Kind == targetKind && targetRef.Name == targetName {
				return true, hpa.Spec.MaxReplicas, nil
			}
		}
	}

	hvpaList := &hvpav1alpha1.HvpaList{}
	if err := h.TargetClient.List(ctx, hvpaList); err != nil && !meta.IsNoMatchError(err) {
		return false, 0, fmt.Errorf("failed to list all HVPAs: %w", err)
	}

	for _, hvpa := range hvpaList.Items {
		if targetRef := hvpa.Spec.TargetRef; targetRef != nil && targetRef.APIVersion == targetAPIVersion &&
			targetRef.Kind == targetKind && targetRef.Name == targetName && hvpa.Spec.Hpa.Deploy {
			return true, hvpa.Spec.Hpa.Template.Spec.MaxReplicas, nil
		}
	}

	return false, 0, nil
}

func (h *Handler) mutateNodeAffinity(
	isZonePinningEnabled bool,
	zones []string,
	podTemplateSpec *corev1.PodTemplateSpec,
) {
	if nodeSelectorRequirement := kubernetesutils.GetNodeSelectorRequirementForZones(isZonePinningEnabled, zones); nodeSelectorRequirement != nil {
		if podTemplateSpec.Spec.Affinity == nil {
			podTemplateSpec.Spec.Affinity = &corev1.Affinity{}
		}

		if podTemplateSpec.Spec.Affinity.NodeAffinity == nil {
			podTemplateSpec.Spec.Affinity.NodeAffinity = &corev1.NodeAffinity{}
		}

		if podTemplateSpec.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
			podTemplateSpec.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = &corev1.NodeSelector{}
		}

		// Filter existing terms with the same expression key to prevent that we are trying to add an expression with
		// the same key multiple times.
		var filteredNodeSelectorTerms []corev1.NodeSelectorTerm
		for _, term := range podTemplateSpec.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
			for _, expr := range term.MatchExpressions {
				if expr.Key != corev1.LabelTopologyZone {
					filteredNodeSelectorTerms = append(filteredNodeSelectorTerms, term)
				}
			}
		}
		podTemplateSpec.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms = filteredNodeSelectorTerms

		if len(podTemplateSpec.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms) == 0 {
			podTemplateSpec.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms = []corev1.NodeSelectorTerm{{}}
		}

		for i := range podTemplateSpec.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
			podTemplateSpec.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[i].MatchExpressions = append(podTemplateSpec.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[i].MatchExpressions, *nodeSelectorRequirement)
		}
	}
}

func (h *Handler) mutateTopologySpreadConstraints(
	failureToleranceType *gardencorev1beta1.FailureToleranceType,
	zones []string,
	isHorizontallyScaled bool,
	currentReplicas *int32,
	maxReplicas int32,
	podTemplateSpec *corev1.PodTemplateSpec,
) {
	replicas := pointer.Int32Deref(currentReplicas, 0)

	// Set maxReplicas to replicas if component is not scaled horizontally or of the replica count is higher than maxReplicas
	// which can happen if the involved H(V)PA object is not mutated yet.
	if !isHorizontallyScaled || replicas > maxReplicas {
		maxReplicas = replicas
	}

	if constraints := kubernetesutils.GetTopologySpreadConstraints(replicas, maxReplicas, metav1.LabelSelector{MatchLabels: podTemplateSpec.Labels}, int32(len(zones)), failureToleranceType); constraints != nil {
		// Filter existing constraints with the same topology key to prevent that we are trying to add a constraint with
		// the same key multiple times.
		var filteredConstraints []corev1.TopologySpreadConstraint
		for _, constraint := range podTemplateSpec.Spec.TopologySpreadConstraints {
			if constraint.TopologyKey != corev1.LabelHostname && constraint.TopologyKey != corev1.LabelTopologyZone {
				filteredConstraints = append(filteredConstraints, constraint)
			}
		}

		podTemplateSpec.Spec.TopologySpreadConstraints = append(filteredConstraints, constraints...)
	}
}

func mutateAutoscalingReplicas(
	log logr.Logger,
	failureToleranceType *gardencorev1beta1.FailureToleranceType,
	obj client.Object,
	getMinReplicas func() *int32,
	setMinReplicas func(*int32),
	getMaxReplicas func() int32,
	setMaxReplicas func(int32),
) error {
	replicas, err := getReplicaCount(obj, getMinReplicas(), failureToleranceType)
	if err != nil {
		return err
	}
	if replicas == nil {
		return nil
	}

	// For compatibility reasons, only overwrite minReplicas if the current count is lower than the calculated count.
	// TODO(timuthy): Reconsider if this should be removed in a future version.
	if pointer.Int32Deref(getMinReplicas(), 0) < *replicas {
		log.Info("Mutating minReplicas", "minReplicas", replicas)
		setMinReplicas(replicas)
	}

	if getMaxReplicas() < pointer.Int32Deref(getMinReplicas(), 0) {
		log.Info("Mutating maxReplicas", "maxReplicas", replicas)
		setMaxReplicas(*replicas)
	}

	return nil
}
