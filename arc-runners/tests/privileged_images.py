#!/usr/bin/env python3
"""Check that every privileged container of the runner pod runs an image pinned by digest."""
from pathlib import Path
import re
import subprocess
import yaml

chart = Path(__file__).resolve().parents[1]
rendered = subprocess.check_output([
    'helm', 'template', 'arc-runners', str(chart), '--namespace', 'arc-runners',
    '--kube-version', '1.31.6',
], text=True)
ars = next(d for d in yaml.safe_load_all(rendered) if d and d['kind'] == 'AutoscalingRunnerSet')
spec = ars['spec']['template']['spec']
containers = spec['initContainers'] + spec['containers']
# A privileged container owns the node. A floating tag is resolved by each pod
# at its own start (OVH forces AlwaysPullImages), so an upstream push would run
# privileged, unreviewed, on the next job: pin the version AND the digest.
pinned = re.compile(r'^[^\s@]+:[^\s@]*\d+\.\d+\.\d+[^\s@]*@sha256:[0-9a-f]{64}$')
privileged = [c for c in containers if (c.get('securityContext') or {}).get('privileged')]
assert privileged, 'no privileged container rendered — this check would pass on nothing'
for c in privileged:
    assert pinned.match(c['image']), f"{c['name']}: privileged image not pinned by version and digest: {c['image']}"
print('PASS: ' + ', '.join(c['name'] for c in privileged) + ' — every privileged container is pinned by version and digest')
