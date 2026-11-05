// Tipos del catálogo de medicamentos (PP-F3-DOM-005, DAT-002, API-003).

export type MedicationForm =
  | 'tablet'
  | 'capsule'
  | 'oral_solution'
  | 'injectable'
  | 'topical_spot_on'
  | 'topical_spray'
  | 'topical_shampoo'
  | 'otic_drops'
  | 'ophthalmic_drops'
  | 'paste'
  | 'powder'

export type MedicationStatus = 'active' | 'discontinued' | 'merged'

export interface ActiveIngredient {
  name: string
  concentration: string
}

export interface Regulatory {
  register_number?: string
  country?: string
  valid_until?: string
}

export interface MedicationSource {
  kind: string
  ref?: string
  imported_at?: string
}

export interface Medication {
  id: string
  name: string
  generic_name?: string
  form: MedicationForm
  concentration: string
  active_ingredients: ActiveIngredient[]
  presentation?: string
  species: string[]
  routes: string[]
  prescription_required: boolean
  controlled: boolean
  withdrawal_days_meat?: number
  withdrawal_days_milk?: number
  regulatory?: Regulatory
  manufacturer?: string
  status: MedicationStatus
  merged_into?: string
  source: MedicationSource
  usage_count: number
  created_at: string
  updated_at: string
}

export interface SuggestMedicationItem {
  catalog_id: string
  name: string
  active_ingredients: ActiveIngredient[]
  form: MedicationForm
  concentration: string
  presentation?: string
  manufacturer?: string
  prescription_required: boolean
  controlled: boolean
  default_route?: string
  default_dose_unit?: string
}

export interface MedicationSuggestion {
  id: string
  proposed_by: string
  organization_id: string
  name: string
  notes?: string
  photo_object_key?: string
  status: 'pending' | 'approved' | 'rejected'
  reviewed_by?: string
  reviewed_at?: string
  review_notes?: string
  created_at: string
}

export interface ImportBatch {
  id: string
  source: string
  total_rows: number
  created: number
  updated: number
  skipped: number
  errors: string[]
  created_by: string
  created_at: string
  reverted_at?: string
}

export interface TopMedicationUsage {
  catalog_id: string
  name: string
  usage_count: number
}

export interface MedicationUsageStats {
  total_plan_items: number
  catalog_linked_items: number
  free_text_items: number
  adoption_rate_pct: number
  top_medications: TopMedicationUsage[]
}

export interface ListMedicationsParams {
  q?: string
  species?: string
  form?: MedicationForm
  prescription_required?: boolean
  status?: MedicationStatus
  limit?: number
  offset?: number
}
