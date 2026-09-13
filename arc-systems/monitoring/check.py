#!/usr/bin/env python3
"""Render the real chart, check discovery wiring, and exercise alert timing.

Requires helm, python3/PyYAML and promtool on PATH. No Kubernetes access.
Run `helm dependency build arc-systems` first, then this script from any cwd.
"""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

import yaml

chart = Path(__file__).resolve().parents[1]

def render(*extra):
    text = subprocess.check_output([
        'helm', 'template', 'arc-systems', str(chart), '--namespace', 'arc-systems', *extra
    ], text=True)
    return [d for d in yaml.safe_load_all(text) if d]

def one(docs, kind):
    found = [d for d in docs if d['kind'] == kind]
    assert len(found) == 1, (kind, len(found))
    return found[0]

docs = render('--set', 'monitoring.labels.release=test-prometheus')
deployment = one(docs, 'Deployment')
pod = deployment['spec']['template']
monitor = one(docs, 'PodMonitor')
rules = one(docs, 'PrometheusRule')
assert all(pod['metadata']['labels'].get(k) == v for k, v in monitor['spec']['selector']['matchLabels'].items())
assert monitor['spec']['namespaceSelector']['matchNames'] == [deployment['metadata']['namespace']]
endpoint = monitor['spec']['podMetricsEndpoints'][0]
assert endpoint['honorLabels'] is True  # retain ARC's scale-set namespace label
manager = next(c for c in pod['spec']['containers'] if c['name'] == 'manager')
assert {'name': endpoint['port'], 'containerPort': 8080, 'protocol': 'TCP'} in manager['ports']
assert '--metrics-addr=:8080' in manager['args']
assert '--listener-metrics-addr=0' in manager['args']
assert '--listener-metrics-endpoint=' in manager['args']
assert monitor['metadata']['labels']['release'] == rules['metadata']['labels']['release'] == 'test-prometheus'
disabled = render('--set', 'monitoring.enabled=false')
assert not any(d['kind'] in ('PodMonitor', 'PrometheusRule') for d in disabled)
with tempfile.TemporaryDirectory(prefix='arc-monitoring-') as tmp:
    tmp = Path(tmp)
    (tmp / 'rules.rendered.yaml').write_text(yaml.safe_dump(rules['spec'], sort_keys=False))
    shutil.copy(chart / 'monitoring/rules.test.yaml', tmp / 'rules.test.yaml')
    promtool = os.environ.get('PROMTOOL', 'promtool')
    subprocess.run([promtool, 'check', 'rules', 'rules.rendered.yaml'], cwd=tmp, check=True)
    subprocess.run([promtool, 'test', 'rules', 'rules.test.yaml'], cwd=tmp, check=True)
print('ARC monitor discovery, metrics flags and 13 alert scenarios passed.')
