// Plantilla del PDF de consulta firmada (PP-F2-ARQ-010, INF-004).
// Los datos llegan en data.json; todos los valores se muestran como texto
// (no se evalúan como marcado). Fuentes: solo las embebidas en Typst.
#let d = json("data.json")

#set document(title: "Consulta " + d.encounter_id)
#set page(
  paper: "a4",
  margin: (x: 18mm, top: 20mm, bottom: 24mm),
  footer: context [
    #set text(size: 7.5pt, fill: rgb("#555555"))
    #grid(
      columns: (1fr, auto),
      align: (left + horizon, right + horizon),
      [Hash de contenido (SHA-256): #raw(d.content_hash) \ Verificación: #d.verify_url],
      [Página #counter(page).display("1 / 1", both: true)],
    )
  ],
)
#set text(lang: "es", size: 10pt)
#set par(justify: false, leading: 0.6em)

#let label(t) = text(size: 8pt, fill: rgb("#666666"), upper(t))
#let section(title) = {
  v(6pt)
  text(size: 11pt, weight: "bold", title)
  line(length: 100%, stroke: 0.5pt + rgb("#bbbbbb"))
}
#let field(name, value) = if value != none and value != "" [
  #label(name) \ #value
]

#grid(
  columns: (1fr, 32mm),
  gutter: 8pt,
  [
    #text(size: 16pt, weight: "bold")[#d.organization_name]
    #v(2pt)
    #text(size: 12pt)[Consulta veterinaria — #d.type_label]
    #v(2pt)
    #text(size: 9pt, fill: rgb("#555555"))[Versión del documento #str(d.document_version)]
  ],
  image("qr.png", width: 30mm),
)

#v(6pt)
#grid(
  columns: (1fr, 1fr, 1fr),
  gutter: 10pt,
  field("Paciente", d.pet_name + " (" + d.pet_species + ")"),
  field("Fecha de inicio", d.started_at),
  field("Profesional responsable", d.attending_vet),
)
#field("Motivo de consulta", d.reason)

#if d.vitals.len() > 0 {
  section("Constantes")
  table(
    columns: (1fr, 1fr),
    stroke: 0.4pt + rgb("#cccccc"),
    ..d.vitals.map(v => (v.label, v.value)).flatten(),
  )
}

#if d.subjective != "" {
  section("S — Anamnesis")
  d.subjective
}

#if d.objective != "" or d.findings.len() > 0 {
  section("O — Exploración")
  if d.objective != "" { d.objective }
  if d.findings.len() > 0 {
    table(
      columns: (1fr, auto, 2fr),
      stroke: 0.4pt + rgb("#cccccc"),
      [*Sistema*], [*Estado*], [*Nota*],
      ..d.findings.map(f => (f.system, f.status, f.note)).flatten(),
    )
  }
}

#if d.diagnoses.len() > 0 {
  section("A — Diagnósticos")
  for x in d.diagnoses [
    - #x.text (#x.certainty)#if x.code != "" [ — #x.code]
  ]
}

#if d.medications.len() > 0 or d.procedures.len() > 0 or d.recommendations != "" {
  section("P — Plan")
  if d.medications.len() > 0 {
    table(
      columns: (2fr, 1fr, 1fr, 1fr, 1fr),
      stroke: 0.4pt + rgb("#cccccc"),
      [*Medicamento*], [*Dosis*], [*Vía*], [*Frecuencia*], [*Duración*],
      ..d.medications.map(m => (m.name, m.dose, m.route, m.frequency, m.duration)).flatten(),
    )
  }
  for p in d.procedures [
    - #p.name#if p.notes != "" [: #p.notes]
  ]
  if d.recommendations != "" {
    v(4pt)
    field("Recomendaciones", d.recommendations)
  }
}

#if d.follow_up != "" {
  section("Control recomendado")
  d.follow_up
}

#if d.attachments.len() > 0 {
  section("Adjuntos")
  for a in d.attachments [
    - #a
  ]
}

#section("Firma")
Firmado por #d.signed_by el #d.signed_at (versión del registro #str(d.stream_version)).

#for ad in d.addenda {
  section("Adenda " + str(ad.seq) + " — " + ad.reason)
  [#ad.text \ ]
  text(size: 9pt, fill: rgb("#555555"))[Firmada por #ad.signed_by el #ad.signed_at]
}
