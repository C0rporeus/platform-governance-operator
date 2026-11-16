# Runbook: adopción de baselines con auditoría continua

**Owner:** Platform Engineering; aceptación del control: SecOps; operación: SRE. **Frecuencia:** cada cambio de baseline o revisión de postura. **Actualizado:** 2026-11-06. **Última ejecución:** consultar evidencia de `TASK-02.7.3` en el backlog; la prueba automatizada se ejecuta en Kind aislado.

## Propósito y requisitos

Adoptar controles mediante `Audit` → revisión del resumen → `Warn` → `Enforce`, incluyendo Pods que existían antes de la política. Ejecutar primero en staging representativo, con el mismo baseline e inyectores previstos para producción.

Se requieren `kubectl`, `jq`, contexto aprobado, permisos de lectura de baseline/status, Pods, eventos y logs del operador, y permisos de edición de la política dentro del cambio autorizado. El operador instalado debe incluir los CRDs actualizados y `get/list/watch pods`. El dueño de aplicación dispone de un plan de rollout/rollback; SecOps define alcance y riesgo aceptable. Asignar personas a estos roles antes de la promoción productiva.

## 1. Fijar contexto, alcance y respaldo

Ejecutar los bloques en una misma sesión de shell y reemplazar los tres valores por los del cambio aprobado:

```bash
audit_context='CONTEXTO-STAGING-APROBADO'
audit_namespace='NAMESPACE-DE-LA-APLICACION'
audit_policy='NOMBRE-DEL-BASELINE'
audit_kind='securitybaseline'
audit_kubectl() { kubectl --context "$audit_context" "$@"; }
audit_kubectl cluster-info
audit_kubectl get "$audit_kind" "$audit_policy" -n "$audit_namespace" -o yaml
audit_evidence_dir=$(mktemp -d /tmp/compliance-change.XXXXXX)
audit_kubectl get "$audit_kind" "$audit_policy" -n "$audit_namespace" -o json > "$audit_evidence_dir/before.json"
```

Para un baseline global usar `audit_kind='clustersecuritybaseline'`: el objeto es cluster-scoped y Kubernetes ignora `-n` para ese recurso. Revisar `namespaceSelector`, exclusiones y selector de Pods; el alcance puede incluir varios equipos. El respaldo contiene el baseline y su status, no los manifiestos de aplicaciones. Conservarlo según la política interna de evidencia, con acceso restringido.

**Esperado:** contexto y política corresponden al cambio; respaldo legible. **Si falla:** corregir contexto/RBAC o nombre; detener la promoción hasta resolverlo.

## 2. Evaluar en Audit

Cambiar el modo solo para una política cuyo cambio esté autorizado; rebajar un baseline corporativo ya aplicado requiere la aceptación de su owner.

```bash
audit_kubectl patch "$audit_kind" "$audit_policy" -n "$audit_namespace" --type=merge \
  -p '{"spec":{"enforcementAction":"Audit"}}'
audit_kubectl get "$audit_kind" "$audit_policy" -n "$audit_namespace" -o json \
  | jq '{generation:.metadata.generation, compliance:.status.compliance,
         condition:[.status.conditions[]? | select(.type=="Compliant")]}'
audit_kubectl get events -A --field-selector "involvedObject.name=$audit_policy" -o json \
  > "$audit_evidence_dir/events.json"
```

Repetir la consulta hasta que `status.compliance.observedGeneration` y la generación de la condición coincidan con `metadata.generation`. El cambio de política dispara reconciliación; además hay revisión cada cinco minutos. Retrasos y fallos pueden extender el plazo: ese intervalo no es un SLO de frescura.

| Resultado | Interpretación | Acción |
| :--- | :--- | :--- |
| `True`, `complete=true`, generaciones iguales | Todos los Pods evaluados satisfacen este baseline | Revisar alcance y conteos antes de promover. |
| `False`, `complete=true` | Hay infracciones confirmadas | El dueño de la aplicación corrige y vuelve a probar. |
| `Unknown`, `AssessmentIncomplete` | Se alcanzó el presupuesto de inspección | Revisar alcance con SecOps; no afirmar cumplimiento global. |
| `Unknown`, `AssessmentFailed` | La revisión falló | SRE revisa logs, RBAC y disponibilidad; repetir tras corregir. |
| Sin summary, generación antigua o condición ausente | No hay evaluación válida de la revisión actual | Esperar/investigar; no promover. |

Los conteos son por Pod; varias reglas incumplidas no multiplican `violating`. Los diez ejemplos son una muestra, no la lista completa. `evaluated=0` no demuestra que una aplicación cumpla: verificar selectores, propietarios y presencia de los Pods esperados.

## 3. Corregir y obtener evidencia actual

Corregir la plantilla o el inyector de la aplicación y ejecutar el rollout de su procedimiento de cambios. El auditor no modifica ni reinicia Pods; un Pod antiguo no cambia porque se cambie la plantilla. Esperar el siguiente ciclo o solicitar una revisión mediante metadatos:

```bash
audit_kubectl annotate "$audit_kind" "$audit_policy" -n "$audit_namespace" --overwrite \
  "operations.platform.f3nr1r.io/audit-request=$(date -u +%Y%m%dT%H%M%SZ)"
audit_kubectl logs -n platform-governance-operator-system \
  -l control-plane=controller-manager --all-containers=true --since=10m --timestamps \
  > "$audit_evidence_dir/operator.log"
audit_kubectl get "$audit_kind" "$audit_policy" -n "$audit_namespace" -o json \
  > "$audit_evidence_dir/assessment.json"
```

**Esperado:** logs de reconciliación/`Assessed existing Pods` correspondientes a la política y al momento de la solicitud; summary completo con generaciones iguales. Ajustar el namespace del operador si la instalación lo personaliza. Una anotación aceptada por sí sola no acredita que la revisión terminó. Un resultado idéntico no reescribe status, por lo que `resourceVersion` o `lastTransitionTime` no sirven como reloj de la última auditoría. Conservar la hora de consulta, logs correlacionados, revisión/digest del operador y commit del cambio.

La comprobación siguiente falla si faltan datos, la revisión es parcial o no hay Pods evaluados:

```bash
jq -e '. as $p | .status.compliance as $s |
  $s.complete == true and $s.observedGeneration == $p.metadata.generation and
  $s.evaluated > 0 and $s.violating == 0 and
  any(.status.conditions[]?; .type == "Compliant" and .status == "True" and
      .observedGeneration == $p.metadata.generation)' "$audit_evidence_dir/assessment.json"
```

Si falla, volver a diagnóstico/corrección; no continuar automáticamente con la promoción.

## 4. Promover a Warn y después Enforce

Tras revisar la evidencia con SecOps y el dueño de aplicación:

```bash
audit_kubectl patch "$audit_kind" "$audit_policy" -n "$audit_namespace" --type=merge \
  -p '{"spec":{"enforcementAction":"Warn"}}'
```

Repetir las consultas y el control de generaciones de los pasos 2 y 3. En staging, probar la admisión de una carga conforme y una no conforme; `Warn` debe permitir ambas y avisar sobre la infractora. Retener salida y códigos. La ausencia de eventos no demuestra cumplimiento; los eventos tienen retención limitada y solo se emiten en transiciones.

Con evidencia revisada y autorización del cambio a enforcement:

```bash
audit_kubectl patch "$audit_kind" "$audit_policy" -n "$audit_namespace" --type=merge \
  -p '{"spec":{"enforcementAction":"Enforce"}}'
```

Revalidar la generación y el resumen; en staging confirmar por dry-run de un Pod que el caso no conforme es rechazado y el conforme es admitido. Para cargas soportadas usar también `hack/ci/check-early-warnings.sh --context "$audit_context" -f <manifiestos-staging>`. Esa compuerta no sustituye la revisión de Pods existentes ni cubre recursos de controladores externos. El auditor no desaloja Pods ya existentes al activar `Enforce`.

## Diagnóstico y límites

| Síntoma | Verificación / recuperación |
| :--- | :--- |
| `AssessmentFailed` o summary ausente | SRE revisa reconciliación y `get/list/watch pods`, namespaces y baselines para la ServiceAccount; no conceder permisos comodín. |
| `AssessmentIncomplete` | Máximo 500 Pods inspeccionados por política/ciclo, incluso los descartados por targeting. No hay cursor de continuación; esperar más ciclos no garantiza cobertura total. SecOps decide un alcance acotado o solicita evolución del motor; no suprimir la señal. |
| Conteos inesperados | Comprobar namespaceSelector, exclusiones, labels/targetRef, Pods terminando y cambios concurrentes. La caché es eventualmente consistente; no es un snapshot transaccional de todo el cluster. |
| Sin evento nuevo | Solo se emite tras persistir un cambio de estado; cambios únicamente de conteo o generación pueden no generar evento. Consultar status y logs. |
| Fallan nuevos Pods tras Enforce | Revisar la regla, mutadores y plantilla; seguir rollback aprobado y verificar recuperación. No borrar baselines ni desactivar webhooks como diagnóstico. |

## Rollback

El dueño del control y SRE deciden si volver al modo anterior reduce el incidente dentro del cambio aprobado. Restaurar únicamente el modo guardado, sin sobrescribir cambios concurrentes de otras reglas:

```bash
audit_restore_patch=$(jq -c '{spec:{enforcementAction:(.spec.enforcementAction // "Enforce")}}' \
  "$audit_evidence_dir/before.json")
audit_kubectl patch "$audit_kind" "$audit_policy" -n "$audit_namespace" --type=merge -p "$audit_restore_patch"
```

Volver a verificar generación, status y admisión en staging. Un cambio de modo no recupera Pods fallidos: el dueño de aplicación ejecuta su recuperación/rollout. Preservar evidencia del cambio y del incidente.

## Escalación y cierre

Ingeniería atiende errores del motor con fixtures de regresión; SRE atiende disponibilidad/RBAC y revisiones fallidas; SecOps decide alcance, excepciones y promoción; el responsable del ISMS valida mapeos normativos y riesgo residual. Usar el ticket y canal de guardia establecidos por el equipo; sus contactos nominales aún deben asignarse en `PLAN-005` §13. Las pruebas técnicas no sustituyen esas aceptaciones.

Cerrar el cambio con resumen y generación, alcance, hora, versión del operador, resultados de admisión, eventos/logs y responsable de aceptación. La prueba automatizada está en `test/e2e/compliance_audit_test.go` y usa `make test-e2e`, kubeconfig propio y Kind descartable.
