#!/usr/bin/env bash
# Change-control gate for platform-governance-operator.
#
# Runs `kubectl apply --dry-run=server` and fails when the early validation of
# workloads reports violations (`Warning: [early] ...`) or the operator rejects them.
# In the default `warn` mode the operator never blocks an apply; this turns its
# warnings into a gate in your pipeline without enabling `enforce` in the cluster.
#
# The early validation fails open (ADR-0003): if the operator is down, an apply
# produces no warnings. A gate that passes in that case verifies nothing, so the
# script first checks that the operator has a ready replica and fails if it cannot.
#
# Usage:
#   check-early-warnings.sh [options] -f manifests/ [other kubectl apply arguments]
#
# Options (all other arguments go to `kubectl apply`):
#   --allow N              tolerate up to N warnings (adopt gradually; lower it over time)
#   --context NAME         kubectl context (used for the check and the apply)
#   --kubeconfig PATH      kubeconfig (used for the check and the apply)
#   --no-operator-check    skip the readiness check (needs `get deployments` in the operator namespace)
#   -h, --help
#
# Environment: KUBECTL (default kubectl), OPERATOR_NAMESPACE
# (platform-governance-operator-system), OPERATOR_DEPLOYMENT
# (platform-governance-operator-controller-manager).
#
# Exit codes: 0 clean; 1 violations (or the operator rejected the apply);
# 2 kubectl failed for another reason or bad usage; 3 the operator is not ready.
set -uo pipefail

KUBECTL="${KUBECTL:-kubectl}"
OPERATOR_NAMESPACE="${OPERATOR_NAMESPACE:-platform-governance-operator-system}"
OPERATOR_DEPLOYMENT="${OPERATOR_DEPLOYMENT:-platform-governance-operator-controller-manager}"

usage() { sed -n '2,/^set -uo/p' "$0" | sed '$d' | sed 's/^# \{0,1\}//'; }

allow=0
check_operator=1
global=()
apply_args=()
while [ $# -gt 0 ]; do
  case "$1" in
    --allow)             allow="${2:-}"; shift 2 ;;
    --context)           global+=(--context "${2:-}"); shift 2 ;;
    --kubeconfig)        global+=(--kubeconfig "${2:-}"); shift 2 ;;
    --no-operator-check) check_operator=0; shift ;;
    -h|--help)           usage; exit 0 ;;
    --)                  shift; apply_args+=("$@"); break ;;
    *)                   apply_args+=("$1"); shift ;;
  esac
done

case "$allow" in ''|*[!0-9]*) echo "error: --allow needs a number" >&2; exit 2 ;; esac
if [ ${#apply_args[@]} -eq 0 ]; then
  echo "error: nothing to apply; pass -f <file|dir> (see --help)" >&2
  exit 2
fi

if [ "$check_operator" = 1 ]; then
  ready="$("$KUBECTL" ${global[@]+"${global[@]}"} -n "$OPERATOR_NAMESPACE" get deployment "$OPERATOR_DEPLOYMENT" \
    -o jsonpath='{.status.readyReplicas}' 2>/dev/null)" || ready=""
  case "$ready" in
    ''|0)
      echo "error: the operator ($OPERATOR_NAMESPACE/$OPERATOR_DEPLOYMENT) has no ready replica or cannot be read." >&2
      echo "       Without it the early validation is skipped silently, so this gate cannot vouch for the apply." >&2
      echo "       Fix the operator, grant 'get deployments' there, or pass --no-operator-check." >&2
      exit 3 ;;
  esac
fi

output="$("$KUBECTL" ${global[@]+"${global[@]}"} apply --dry-run=server "${apply_args[@]}" 2>&1)"
status=$?
printf '%s\n' "$output"

early="$(printf '%s\n' "$output" | grep -c '^Warning: \[early\]')"
rejected="$(printf '%s\n' "$output" | grep -cE 'admission webhook "(vworkload|vpod)[^"]*" denied|violates SecurityBaseline')"

if [ "$status" -ne 0 ]; then
  if [ "$rejected" -gt 0 ] || [ "$early" -gt 0 ]; then
    echo "gate: FAILED, the operator rejected the apply (see above)." >&2
    exit 1
  fi
  echo "gate: kubectl failed (exit $status) for a reason unrelated to policy." >&2
  exit 2
fi

if [ "$early" -gt "$allow" ]; then
  echo "gate: FAILED, $early early-validation warning(s), $allow allowed." >&2
  exit 1
fi
echo "gate: passed ($early early-validation warning(s), $allow allowed)." >&2
exit 0
