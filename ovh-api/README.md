# ovh-api — CLI access to the OVH API v1 (kube control plane)

Signed API calls without going through the web console
(https://eu.api.ovh.com/console/?section=%2Fcloud&branch=v1), which requires a
browser OAuth login every time the page resets.

## One-time setup: create scoped API credentials

Open (logged in to the OVH account that owns the Public Cloud project):

```
https://www.ovh.com/auth/api/createToken/?GET=%2Fcloud%2Fproject%2F%2A%2Fkube%2F%2A&POST=%2Fcloud%2Fproject%2F%2A%2Fkube%2F%2A
```

This requests GET + POST on `/cloud/project/*/kube/*` only. Save the three
values (application key, application secret, consumer key) — **write them
yourself**, never paste them into a chat or terminal history — into
`~/lab/fabrique/.secrets/ovh-api.env`:

```sh
export OVH_AK=<application_key>
export OVH_AS=<application_secret>
export OVH_CK=<consumer_key>
```

`chmod 600` the file. The script sources it; it never prints the values.

## Usage

```sh
ovh-api/ovh-kube-restart.sh names             # identify clusters (id name region version status)
ovh-api/ovh-kube-restart.sh status <kubeId>   # full cluster document
ovh-api/ovh-kube-restart.sh restart <kubeId>  # POST restart, force=false (apiserver only, no downtime)
ovh-api/ovh-kube-restart.sh watch <kubeId>    # poll until READY
```

`force=true` redeploys the whole control plane with a brief apiserver outage —
avoid unless OVH support asks for it.

Cluster IDs and the incident runbook live in the repo [CLAUDE.md](../CLAUDE.md)
(§ "OVH API — kube control plane restart").
