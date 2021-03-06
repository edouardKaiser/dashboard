// Copyright 2015 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package deployment

import (
	"log"

	"github.com/kubernetes/dashboard/src/app/backend/resource/common"
	"github.com/kubernetes/dashboard/src/app/backend/resource/event"

	"k8s.io/kubernetes/pkg/api"
	k8serrors "k8s.io/kubernetes/pkg/api/errors"
	"k8s.io/kubernetes/pkg/apis/extensions"
	client "k8s.io/kubernetes/pkg/client/unversioned"
)

// ReplicationSetList contains a list of Deployments in the cluster.
type DeploymentList struct {
	ListMeta common.ListMeta `json:"listMeta"`

	// Unordered list of Deployments.
	Deployments []Deployment `json:"deployments"`
}

// Deployment is a presentation layer view of Kubernetes Deployment resource. This means
// it is Deployment plus additional augumented data we can get from other sources
// (like services that target the same pods).
type Deployment struct {
	ObjectMeta common.ObjectMeta `json:"objectMeta"`
	TypeMeta   common.TypeMeta   `json:"typeMeta"`

	// Aggregate information about pods belonging to this Deployment.
	Pods common.PodInfo `json:"pods"`

	// Container images of the Deployment.
	ContainerImages []string `json:"containerImages"`
}

// GetDeploymentList returns a list of all Deployments in the cluster.
func GetDeploymentList(client client.Interface, nsQuery *common.NamespaceQuery,
	dsQuery *common.DataSelectQuery) (*DeploymentList, error) {
	log.Printf("Getting list of all deployments in the cluster")

	channels := &common.ResourceChannels{
		DeploymentList: common.GetDeploymentListChannel(client.Extensions(), nsQuery, 1),
		PodList:        common.GetPodListChannel(client, nsQuery, 1),
		EventList:      common.GetEventListChannel(client, nsQuery, 1),
	}

	return GetDeploymentListFromChannels(channels, dsQuery)
}

// GetDeploymentList returns a list of all Deployments in the cluster
// reading required resource list once from the channels.
func GetDeploymentListFromChannels(channels *common.ResourceChannels,
	dsQuery *common.DataSelectQuery) (*DeploymentList, error) {

	deployments := <-channels.DeploymentList.List
	if err := <-channels.DeploymentList.Error; err != nil {
		statusErr, ok := err.(*k8serrors.StatusError)
		if ok && statusErr.ErrStatus.Reason == "NotFound" {
			// NotFound - this means that the server does not support Deployment objects, which
			// is fine.
			emptyList := &DeploymentList{
				Deployments: make([]Deployment, 0),
			}
			return emptyList, nil
		}
		return nil, err
	}

	pods := <-channels.PodList.List
	if err := <-channels.PodList.Error; err != nil {
		return nil, err
	}

	events := <-channels.EventList.List
	if err := <-channels.EventList.Error; err != nil {
		return nil, err
	}

	return CreateDeploymentList(deployments.Items, pods.Items, events.Items, dsQuery), nil
}

// CreateDeploymentList returns a list of all Deployment model objects in the cluster, based on all
// Kubernetes Deployment API objects.
func CreateDeploymentList(deployments []extensions.Deployment, pods []api.Pod,
	events []api.Event, dsQuery *common.DataSelectQuery) *DeploymentList {

	deploymentList := &DeploymentList{
		Deployments: make([]Deployment, 0),
		ListMeta:    common.ListMeta{TotalItems: len(deployments)},
	}

	deployments = fromCells(common.GenericDataSelect(toCells(deployments), dsQuery))

	for _, deployment := range deployments {

		matchingPods := common.FilterNamespacedPodsBySelector(pods, deployment.ObjectMeta.Namespace,
			deployment.Spec.Selector.MatchLabels)
		podInfo := common.GetPodInfo(deployment.Status.Replicas, deployment.Spec.Replicas,
			matchingPods)
		podInfo.Warnings = event.GetPodsEventWarnings(events, matchingPods)

		deploymentList.Deployments = append(deploymentList.Deployments,
			Deployment{
				ObjectMeta:      common.NewObjectMeta(deployment.ObjectMeta),
				TypeMeta:        common.NewTypeMeta(common.ResourceKindDeployment),
				ContainerImages: common.GetContainerImages(&deployment.Spec.Template.Spec),
				Pods:            podInfo,
			})
	}

	return deploymentList
}
