#!/usr/bin/env bash
# Resumen y validación del tracking de specs.
#   scripts/specs-status.sh              resumen de todas las fases
#   scripts/specs-status.sh --phase 1    resumen de una fase
#   scripts/specs-status.sh --check      validación (exit != 0 si hay errores)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DOCS="$ROOT/docs"
VALID_STATES="PENDIENTE EN_DISENO EN_PROGRESO IMPLEMENTADO VERIFICADO BLOQUEADO DESCARTADO"
ID_RE='PP-F[0-9]+-(PRD|DOM|ARQ|IAM|API|DAT|FE|NOT|SEC|INF|QA)-[0-9]{3}'

mode="summary"; phase=""
while [ $# -gt 0 ]; do
  case "$1" in
    --check) mode="check" ;;
    --phase) phase="$2"; shift ;;
    -h|--help) sed -n '2,5p' "$0"; exit 0 ;;
    *) echo "argumento desconocido: $1" >&2; exit 2 ;;
  esac
  shift
done

tracking_files() {
  if [ -n "$phase" ]; then ls "$DOCS/tracking/phase-$phase.md" 2>/dev/null || true
  else ls "$DOCS"/tracking/phase-*.md 2>/dev/null || true; fi
}

# Filas de la tabla: ID|Área|Prio|Hito|Estado|Notas
rows() {
  grep -E "^\| *PP-F[0-9]+-" "$1" | awk -F'|' '{
    for (i=1;i<=NF;i++){ gsub(/^[ \t]+|[ \t]+$/,"",$i) }
    print $2 "|" $3 "|" $5 "|" $6 "|" $7 "|" $10
  }'
}

summary() {
  local f="$1" name
  name="$(basename "$f" .md)"
  echo "== $name =="
  local total; total=$(rows "$f" | wc -l | tr -d ' ')
  echo "Total requisitos: $total"
  echo
  echo "Por estado:"
  for s in $VALID_STATES; do
    local n; n=$(rows "$f" | awk -F'|' -v s="$s" '$5==s' | wc -l | tr -d ' ')
    [ "$n" -gt 0 ] && printf "  %-13s %4d\n" "$s" "$n"
  done
  local done_n; done_n=$(rows "$f" | awk -F'|' '$5=="IMPLEMENTADO"||$5=="VERIFICADO"' | wc -l | tr -d ' ')
  local must_total must_ver
  must_total=$(rows "$f" | awk -F'|' '$3=="MUST"' | wc -l | tr -d ' ')
  must_ver=$(rows "$f" | awk -F'|' '$3=="MUST" && $5=="VERIFICADO"' | wc -l | tr -d ' ')
  echo
  [ "$total" -gt 0 ] && echo "Avance (implementado+verificado): $done_n/$total ($((done_n*100/total))%)"
  [ "$must_total" -gt 0 ] && echo "MUST verificados (cierre de fase): $must_ver/$must_total ($((must_ver*100/must_total))%)"
  echo
  echo "Por área (impl.+verif. / total):"
  rows "$f" | awk -F'|' '{ t[$2]++; if ($5=="IMPLEMENTADO"||$5=="VERIFICADO") d[$2]++ }
    END { for (a in t) printf "  %-4s %3d/%3d\n", a, d[a]+0, t[a] }' | sort
  echo
  echo "Por hito (impl.+verif. / total):"
  rows "$f" | awk -F'|' '{ t[$4]++; if ($5=="IMPLEMENTADO"||$5=="VERIFICADO") d[$4]++ }
    END { for (h in t) printf "  %-5s %3d/%3d\n", h, d[h]+0, t[h] }' | sort
  local blocked; blocked=$(rows "$f" | awk -F'|' '$5=="BLOQUEADO"')
  if [ -n "$blocked" ]; then
    echo; echo "Bloqueados:"; echo "$blocked" | awk -F'|' '{ printf "  %s — %s\n", $1, ($6==""?"(sin nota)":$6) }'
  fi
  echo
}

check() {
  local f="$1" errors=0 n
  n="$(basename "$f" .md | sed 's/phase-//')"
  local specdir="$DOCS/phase-$n"
  echo "== check phase-$n =="

  # 1. estados válidos
  while IFS='|' read -r id area prio hito state notes; do
    case " $VALID_STATES " in *" $state "*) ;; *)
      echo "  ERROR estado inválido '$state' en $id"; errors=$((errors+1)) ;;
    esac
    if [ "$prio" = "MUST" ] && [ "$state" = "DESCARTADO" ] && [ -z "$notes" ]; then
      echo "  ERROR $id es MUST DESCARTADO sin nota"; errors=$((errors+1))
    fi
  done < <(rows "$f")

  # 2. duplicados
  local dups; dups=$(rows "$f" | cut -d'|' -f1 | sort | uniq -d)
  if [ -n "$dups" ]; then echo "  ERROR IDs duplicados en tracking: $dups"; errors=$((errors+1)); fi

  # 3. consistencia specs <-> tracking
  if [ -d "$specdir" ]; then
    local in_specs in_track
    in_specs=$(grep -rhoE "\*\*\[$ID_RE\]" "$specdir" --include='*.md' | tr -d '*[]' | sort -u)
    in_track=$(rows "$f" | cut -d'|' -f1 | sort -u)
    local missing_track; missing_track=$(comm -23 <(echo "$in_specs") <(echo "$in_track"))
    local missing_spec;  missing_spec=$(comm -13 <(echo "$in_specs") <(echo "$in_track"))
    if [ -n "$missing_track" ]; then
      echo "  ERROR definidos en specs pero ausentes en tracking:"; echo "$missing_track" | sed 's/^/    /'; errors=$((errors+1))
    fi
    if [ -n "$missing_spec" ]; then
      echo "  ERROR en tracking pero sin definición **[ID]** en specs:"; echo "$missing_spec" | sed 's/^/    /'; errors=$((errors+1))
    fi
    local dup_defs; dup_defs=$(grep -rhoE "\*\*\[$ID_RE\]" "$specdir" --include='*.md' | tr -d '*[]' | sort | uniq -d)
    if [ -n "$dup_defs" ]; then echo "  ERROR IDs definidos más de una vez en specs: $dup_defs"; errors=$((errors+1)); fi
  else
    echo "  WARN no existe $specdir"
  fi

  if [ "$errors" -eq 0 ]; then echo "  OK"; fi
  return "$errors"
}

files="$(tracking_files)"
if [ -z "$files" ]; then echo "No hay archivos de tracking (docs/tracking/phase-N.md)" >&2; exit 1; fi

rc=0
for f in $files; do
  if [ "$mode" = "check" ]; then check "$f" || rc=1
  else summary "$f"; fi
done
exit $rc
