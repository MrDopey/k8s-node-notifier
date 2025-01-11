package v1

import "k8s.io/apimachinery/pkg/runtime"

// DeepCopyInto copies all properties of this object into another object of the
// same type that is provided as a pointer.
func (in *NodeNotifier) DeepCopyInto(out *NodeNotifier) {
	out.TypeMeta = in.TypeMeta
	out.ObjectMeta = in.ObjectMeta
	out.Spec = NodeNotifierSpec{
		Label:    in.Spec.Label,
		SlackUrl: in.Spec.SlackUrl,
	}
}

// DeepCopyObject returns a generically typed copy of an object
func (in *NodeNotifier) DeepCopyObject() runtime.Object {
	out := NodeNotifier{}
	in.DeepCopyInto(&out)

	return &out
}

// DeepCopyObject returns a generically typed copy of an object
func (in *NodeNotifierList) DeepCopyObject() runtime.Object {
	out := NodeNotifierList{}
	out.TypeMeta = in.TypeMeta
	out.ListMeta = in.ListMeta

	if in.Items != nil {
		out.Items = make([]NodeNotifier, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}

	return &out
}
