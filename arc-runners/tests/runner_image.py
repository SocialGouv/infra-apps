#!/usr/bin/env python3
"""Check that every container executing the runner distribution runs one pinned image."""
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
image = {c['name']: c['image'] for c in spec['initContainers'] + spec['containers']}
# One immutable version and its digest: a floating tag is resolved per pod at
# its own start, and a stale runner deadlocks the scale set silently.
pinned = re.compile(r'^ghcr\.io/socialgouv/iterion-ci-runner:\d+\.\d+\.\d+@sha256:[0-9a-f]{64}$')
assert pinned.match(image['runner']), image['runner']
# The externals the init copies are executed by the runner: one distribution,
# one image, on both containers.
assert image['init-dind-externals'] == image['runner'], (image['init-dind-externals'], image['runner'])
print('PASS: the runner image is an immutable iterion-ci-runner version pinned by digest')
print('PASS: init-dind-externals copies the externals of the very image the runner executes')
