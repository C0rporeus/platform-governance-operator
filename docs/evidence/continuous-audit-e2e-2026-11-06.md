# Evidencia de `TASK-02.7.3` — 2026-11-06

- Entorno: Docker Desktop 29.9.2, Kind descartable de tres nodos, imagen `kindest/node:v1.37.0`, cert-manager v1.19.3.
- Aislamiento: cluster `platform-governance-operator-test-e2e`, kubeconfig `bin/e2e-kubeconfig`, contexto verificado por `BeforeSuite`. El cluster fue eliminado automáticamente tras la ejecución satisfactoria.
- Código: árbol de trabajo local con cambios sin commit; HEAD base `9def74d10580847168ca5b654664edebd033367c`. No representa un release publicado.
- Comando: `make test-e2e`.
- Resultado: **28/28 specs aprobadas, 0 fallidas, 0 pendientes, 0 omitidas; 559.422 segundos de suite**.
- Complementos: `make test` satisfactorio; `./bin/golangci-lint run`: 0 hallazgos; `git diff --check` y sintaxis bash de los bloques del runbook satisfactorios.

## Casos nuevos comprobados

`test/e2e/compliance_audit_test.go` añade dos specs:

1. Pod anterior al baseline detectado en Audit, summary y ejemplos, evento `ViolationsFound`, reemplazo del Pod sin editar la política, convergencia por el ciclo periódico real de cinco minutos, estado True y evento `AssessmentPassed`, promoción a Warn/Enforce con generación actual, denegación de un Pod infractor y ausencia de reescrituras de status tras una revisión de metadatos sin cambios de resultado. Duración del spec: 313.237 segundos.
2. Summary y condición de `ClusterSecurityBaseline` persistidos para el namespace seleccionado, con ejemplo del Pod anterior a la política. Duración: 0.212 segundos.

La suite completa también conserva las regresiones existentes de admisión, eventos, HA, defaults y dry-run con salvaguarda. La prueba verifica la escritura de eventos y status con RBAC real del despliegue.

## Artefactos

- [Runbook](../RUNBOOK-COMPLIANCE.md), SHA-256 `83c70fbf0df8831ae50a3323d146fe211554ee700c423ee1d281809f8e587669`.
- Prueba E2E, SHA-256 `4fe4e13c67d0eadef5e33fe0d396d57d162a59438014c712a7c91009ab439992`.

## Límites de la evidencia

Validación en una versión de Kubernetes/Kind; no demuestra toda la matriz propuesta de plataformas. Los estados Unknown, presupuesto y fallos se prueban en unit/integration, no mediante un corte de permisos en este E2E. El procedimiento manual completo de rollback y su aceptación por un operador distinto del autor no fueron ejecutados aquí. La prueba automatizada cubre el flujo técnico de adopción; no acredita certificación ni aceptación nominal de riesgo por el ISMS. `TASK-02.4.4` sigue pendiente como cierre integral de CEL/validación temprana.
