# seaweedfs chart 4.48.0 — vendored, patched

Upstream: https://seaweedfs.github.io/seaweedfs/helm, chart `seaweedfs` 4.48.0
(appVersion 4.48, git tag 4.48 = 530be3e37337488ecc34d58441e0bc476e121c93).

One change: `templates/shared/security-configmap.yaml` is deleted. It calls
`fromToml`, which only Helm >= 3.17 knows; ArgoCD-SRE (v2.14.x) renders with
Helm 3.16.3 and parses every template of a loaded subchart, even one that
renders nothing, so the whole iterion-prod app fails to render with it. With
`global.seaweedfs.securityConfig.jwtSigning` off (values.ovh-prod.yaml) the
template renders nothing here anyway.

`../../Chart.yaml` depends on this directory (`file://vendor-charts/seaweedfs`),
so `helm dependency update` packages it — patch included — into
`charts/seaweedfs-4.48.0.tgz`. To move to a newer chart: replace this directory
with the new upstream chart, delete the same template again (and any other
function Helm 3.16.3 lacks), then render with ArgoCD's own helm
(`quay.io/argoproj/argocd:<the ArgoCD-SRE version>`) before pushing.
