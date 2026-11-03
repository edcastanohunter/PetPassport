export type SearchMode = 'full' | 'degraded'

export interface FreshnessInfo {
  lag_seconds: number
}

export interface FacetBucket {
  key: string
  count: number
}

export interface PatientSearchResultItem {
  pet: {
    id: string
    name: string
    species: string
    breed?: string
    sex: string
    microchip_suffix?: string
    status: string
    photo_thumb_url?: string
  }
  guardian_display_name?: string
  access_level: 'linked' | 'custody' | 'guardian' | string
  last_encounter_at?: string
  highlight?: Record<string, string>
}

export interface PatientSearchResponse {
  data: PatientSearchResultItem[]
  facets: Record<string, FacetBucket[]>
  next_cursor?: string | null
  freshness: FreshnessInfo
  mode: SearchMode
}

export interface DiagnosisItem {
  text: string
  certainty: 'presumptive' | 'confirmed' | string
}

export interface ClinicalHitItem {
  doc_kind: string
  ref_id: string
  occurred_at: string
  organization: {
    id?: string
    name?: string
  }
  encounter_type: string
  diagnoses?: DiagnosisItem[]
  highlight?: Record<string, string[]>
}

export interface ClinicalSearchResultItem {
  pet: {
    id: string
    name: string
    species: string
  }
  hits: ClinicalHitItem[]
}

export interface ClinicalSearchResponse {
  data: ClinicalSearchResultItem[]
  facets: Record<string, FacetBucket[]>
  next_cursor?: string | null
  freshness: FreshnessInfo
  mode: SearchMode
}

export interface SuggestPatientItem {
  pet_id: string
  name: string
  species: string
  guardian_display_name?: string
  microchip_suffix?: string
  access_level: 'linked' | 'custody' | string
}

export interface SuggestPatientsResponse {
  data: SuggestPatientItem[]
}

export interface SuggestPetMeItem {
  pet_id: string
  name: string
  species: string
  microchip_suffix?: string
}

export interface SuggestPetsMeResponse {
  data: SuggestPetMeItem[]
}
