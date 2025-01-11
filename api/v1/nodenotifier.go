package v1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type NodeNotifierList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []NodeNotifier `json:"items"`
}

type NodeNotifier struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec NodeNotifierSpec `json:"spec"`
}

type NodeNotifierSpec struct {
	Label    string `json:"label"`
	SlackUrl string `json:"slack-url"`
}
