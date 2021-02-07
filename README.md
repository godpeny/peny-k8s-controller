# peny-k8s-controller

## init
```
kubebuilder init --domain peny.k8s.com
kubebuilder create api --group godpeny --version v1 --kind PenyCrd
```

## reference
 - https://book.kubebuilder.io/cronjob-tutorial/controller-implementation.html
 - https://ssup2.github.io/programming/Kubernetes_Kubebuilder/
 - https://github.com/kubernetes-sigs/kubebuilder/issues/1270
 - https://github.com/kubernetes/cloud-provider/blob/3747c6100d162d02cbe1ac6cb72b96d6288718ad/controllers/service/controller.go#L158
 - https://stuartleeks.com/posts/kubebuilder-event-filters-part-1-delete/
 - https://stuartleeks.com/posts/kubebuilder-event-filters-part-2-update/