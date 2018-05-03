package main

import (
	"fmt"

	"k8s.io/client-go/pkg/api/meta"
	"k8s.io/client-go/pkg/api/v1"
	metav1 "k8s.io/client-go/pkg/apis/meta/v1"
	"k8s.io/client-go/pkg/runtime/schema"
	"k8s.io/client-go/pkg/types"
)

type PodToService struct {
	metav1.TypeMeta `json:",inline"`
	Metadata v1.ObjectMeta `json:"matadata"`

	PodName string `json:"podName"`
	PodAddress string `json:"podAddress"`
	PodUID types.UID `json:"podUID"`
	ServiceName string `json:"serviceName,omitempty"`
}

func (p PodToService) String() string {
	return fmt.Sprintf("Pod [%s:%s] is backing service %s\n",p.Metadata.Name, p.PodAddress, p.ServiceName)
}

type PodToServiceList struct {
	metav1.TypeMeta `json:",inline"`
	Metadata metav1.ListMeta `json:"metadata"`

	Items []PodToService `json:"items"`
}

func (p *PodToService) GetObjectKind() schema.ObjectKind {
	return &p.TypeMeta
}

func (p *PodToService) GetObjectMeta() meta.Object {
	return &p.Metadata
}

func (pl *PodToServiceList) GetObjectKind() schema.ObjectKind {
	return &pl.TypeMeta
}

func (pl *PodToServiceList) GetObjectMeta() metav1.List {
	return &pl.Metadata
}


type PodToServiceListCopy PodToServiceList
type PodToServiceCopy PodToService

func (p *PodToService) UnmarshalJSON(data []byte) error {
	tmp := PodToServiceCopy{}
	err := json.Unmarshal(data, &tmp)
	if err != nil {
		return err
	}
	tmp2 := PodToService(tmp)
	*p = tmp2
	return nil 
}

func (pl *PodToServiceList) UnmarshalJSON(data []byte) error {
	tmp := PodToServiceListCopy{}
	err := json.Unmarshal(data, &tmp)
	if err != nil {
		return nil 
	}
	tmp2 := PodToServiceList(tmp)
	*pl = tmp2
	return nil
}
 


