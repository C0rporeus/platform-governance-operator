#!/usr/bin/env bash

set -e

echo "Running E2E tests using Kind..."

# Create kind cluster if it doesn't exist
if ! kind get clusters | grep -q "governance-test"; then
  kind create cluster --name governance-test
fi

# Load image into kind
make docker-build IMG=governance-operator:test
kind load docker-image governance-operator:test --name governance-test

# Deploy operator
make deploy IMG=governance-operator:test

echo "Waiting for operator to be ready..."
kubectl wait --for=condition=available --timeout=60s deployment/platform-governance-operator-controller-manager -n platform-governance-operator-system

echo "Applying SecurityBaseline..."
cat <<EOF | kubectl apply -f -
apiVersion: core.platform.f3nr1r.io/v1alpha1
kind: SecurityBaseline
metadata:
  name: strict-security
spec:
  runAsNonRoot: true
  readOnlyRootFilesystem: true
EOF

echo "Testing Pod creation..."
cat <<EOF > test-pod.yaml
apiVersion: v1
kind: Pod
metadata:
  name: test-pod
  namespace: default
spec:
  containers:
  - name: nginx
    image: nginx
EOF

if kubectl apply -f test-pod.yaml; then
  echo "❌ Pod creation succeeded, but it should have been rejected by SecurityBaseline!"
  exit 1
else
  echo "✅ Pod creation correctly rejected by SecurityBaseline."
fi

echo "E2E tests passed."
