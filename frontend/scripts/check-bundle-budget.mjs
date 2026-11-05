import fs from 'node:fs'
import path from 'node:path'
import zlib from 'node:zlib'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const distDir = path.resolve(__dirname, '../dist/assets')

if (!fs.existsSync(distDir)) {
  console.error('Error: dist/assets no existe. Ejecuta `npm run build` primero.')
  process.exit(1)
}

function getGzipSize(filePath) {
  const content = fs.readFileSync(filePath)
  return zlib.gzipSync(content).length
}

const files = fs.readdirSync(distDir)
let indexGz = 0
let vendorGz = 0
let uiGz = 0
let dashboardGz = 0
let clinicalEditorGz = 0

for (const file of files) {
  const filePath = path.join(distDir, file)
  if (!file.endsWith('.js')) continue
  const size = getGzipSize(filePath)

  if (file.startsWith('index-')) indexGz = size
  else if (file.startsWith('vendor-')) vendorGz = size
  else if (file.startsWith('ui-')) uiGz = size
  else if (file.startsWith('DashboardPage-')) dashboardGz = size
  else if (file.startsWith('EncounterEditorPage-')) clinicalEditorGz = size
}

const appInitialGz = indexGz + vendorGz + dashboardGz
const clinicalInitialGz = indexGz + vendorGz + uiGz + clinicalEditorGz

const APP_BUDGET_KB = 180
const CLINICAL_BUDGET_KB = 250
const MARGIN = 1.1 // +10% de tolerancia antes de fallar (PP-F3-FE-010)

const appInitialKb = appInitialGz / 1024
const clinicalInitialKb = clinicalInitialGz / 1024

console.log('========================================================')
console.log('PetPassport — Presupuesto de Bundle Frontend (PP-F3-FE-010)')
console.log('========================================================')
console.log(
  `Ruta /app inicial:       ${appInitialKb.toFixed(2)} KB gz (Presupuesto: ${APP_BUDGET_KB} KB, Máx +10%: ${(APP_BUDGET_KB * MARGIN).toFixed(1)} KB)`,
)
console.log(
  `Espacio clínico:         ${clinicalInitialKb.toFixed(2)} KB gz (Presupuesto: ${CLINICAL_BUDGET_KB} KB, Máx +10%: ${(CLINICAL_BUDGET_KB * MARGIN).toFixed(1)} KB)`,
)
console.log('--------------------------------------------------------')

let failed = false
if (appInitialKb > APP_BUDGET_KB * MARGIN) {
  console.error(
    `❌ FALLO: La ruta /app supera el presupuesto en más del 10% (${appInitialKb.toFixed(2)} KB > ${(APP_BUDGET_KB * MARGIN).toFixed(1)} KB)`,
  )
  failed = true
} else {
  console.log('✅ Ruta /app dentro del presupuesto.')
}

if (clinicalInitialKb > CLINICAL_BUDGET_KB * MARGIN) {
  console.error(
    `❌ FALLO: El espacio clínico supera el presupuesto en más del 10% (${clinicalInitialKb.toFixed(2)} KB > ${(CLINICAL_BUDGET_KB * MARGIN).toFixed(1)} KB)`,
  )
  failed = true
} else {
  console.log('✅ Espacio clínico dentro del presupuesto.')
}
console.log('========================================================')

if (failed) {
  process.exit(1)
}
