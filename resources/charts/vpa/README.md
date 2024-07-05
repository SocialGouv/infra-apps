# VerticalPodAutoscaler

## official kubernetes documentation

https://github.com/kubernetes/autoscaler/blob/master/vertical-pod-autoscaler

## implementation

- disabled vpa-updater + admission-controller(+mutatingwebhook), so mode `Auto`,`Initial` and `Recreate` are disabled as we don't use it (we use oblik instead), only mode `Off` (audit and provide recommendations) is available.

## some reading

- https://medium.com/infrastructure-adventures/vertical-pod-autoscaler-deep-dive-limitations-and-real-world-examples-9195f8422724
- https://foxutech.medium.com/vertical-pod-autoscaler-vpa-know-everything-about-it-6a2d7a383268
- https://foxutech.com/vertical-pod-autoscalervpa-know-everything-about-it/
- https://medium.com/@kewynakshlley/performance-evaluation-of-the-autoscaling-strategies-vertical-and-horizontal-using-kubernetes-42d9a1663e6b
- https://caiolombello.medium.com/kubernetes-hpa-custom-metrics-for-effective-cpu-memory-scaling-23526bba9b4
- https://kubernetes.io/docs/concepts/workloads/autoscaling/
- https://overcast.blog/11-ways-to-optimize-kubernetes-vertical-pod-autoscaler-930246954fc4
- https://medium.com/@kewynakshlley/performance-evaluation-of-the-autoscaling-strategies-vertical-and-horizontal-using-kubernetes-42d9a1663e6b
- https://github.com/kubernetes/autoscaler/blob/master/multidimensional-pod-autoscaler/AEP.md
- https://cloud.google.com/kubernetes-engine/docs/how-to/multidimensional-pod-autoscaling?hl=fr
