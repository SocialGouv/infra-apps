#!/usr/bin/env python3
"""Check the effective ARC startup contract after the upstream chart renders."""
from pathlib import Path
import subprocess
import yaml

chart = Path(__file__).resolve().parents[1]
rendered = subprocess.check_output([
    'helm', 'template', 'arc-runners', str(chart), '--namespace', 'arc-runners',
    '--kube-version', '1.31.6',
], text=True)
ars = next(d for d in yaml.safe_load_all(rendered) if d and d['kind'] == 'AutoscalingRunnerSet')
spec = ars['spec']['template']['spec']
names = [c['name'] for c in spec['initContainers']]
assert names == ['configure-inotify', 'init-dind-externals', 'dind'], names
assert [c['name'] for c in spec['containers']] == ['runner']
dind = spec['initContainers'][-1]
runner = spec['containers'][0]
assert dind['restartPolicy'] == 'Always'
probe = dind['startupProbe']
assert probe['exec']['command'] == ['docker', '--host=unix:///run/docker/docker.sock', 'info']
assert probe['periodSeconds'] > 0 and probe['timeoutSeconds'] > 0 and probe['failureThreshold'] > 0
runner_host = next(e['value'] for e in runner['env'] if e['name'] == 'DOCKER_HOST')
assert '--host=' + runner_host in dind['args']
assert '--host=' + runner_host in probe['exec']['command']
for container in (dind, runner):
    assert any(v['name'] == 'dind-sock' and v['mountPath'] == '/run/docker' for v in container['volumeMounts'])
assert runner['command'] == ['/home/runner/run.sh']
print('PASS: native dind sidecar must pass an API startup probe before runner starts')
print('PASS: runner, daemon and probe use the same shared Docker socket')
