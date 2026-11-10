export type ServiceStatus =
  'operational' | 'degraded_performance' | 'partial_outage' | 'major_outage' | 'maintenance'

export type IncidentSeverity = 'S1' | 'S2' | 'S3' | 'S4'

export interface ComponentStatus {
  id: string
  name: string
  status: ServiceStatus
  uptime_percentage_90d: number
}

export interface IncidentUpdate {
  id: string
  status: string
  body: string
  created_at: string
}

export interface Incident {
  id: string
  name: string
  status: 'investigating' | 'identified' | 'monitoring' | 'resolved'
  severity: IncidentSeverity
  impact: string
  created_at: string
  updated_at: string
  updates: IncidentUpdate[]
}

export interface ScheduledMaintenance {
  id: string
  name: string
  status: 'scheduled' | 'in_progress' | 'completed'
  scheduled_for: string
  scheduled_until: string
  impact: string
  description: string
}

export interface SystemStatusData {
  status: ServiceStatus
  updated_at: string
  components: ComponentStatus[]
  active_incidents: Incident[]
  scheduled_maintenances: ScheduledMaintenance[]
}
