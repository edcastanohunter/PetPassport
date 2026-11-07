// Plantilla del PDF de receta médica veterinaria (PP-F4-PRD-004, ARQ-011).
// Los datos llegan en data.json; todos los valores se muestran como texto.
#let d = json("data.json")

#set document(title: "Receta Médica Veterinaria - " + d.pet_name)
#set page(
  paper: "a4",
  margin: (x: 18mm, top: 20mm, bottom: 24mm),
  footer: context [
    #set text(size: 7.5pt, fill: rgb("#555555"))
    #grid(
      columns: (1fr, auto),
      align: (left + horizon, right + horizon),
      [
        Huella canónica de contenido (SHA-256): #raw(d.content_hash) \
        Verificación de autenticidad: #d.verify_url
      ],
      [Página #counter(page).display("1 / 1", both: true)],
    )
  ],
)
#set text(lang: "es", size: 10pt)

#grid(
  columns: (1fr, 34mm),
  gutter: 10pt,
  [
    #text(size: 18pt, weight: "bold", fill: rgb("#1e3a8a"))[Receta Médica Veterinaria]
    #v(3pt)
    #text(size: 11pt, weight: "bold")[#d.org_name]
    #v(1pt)
    #text(size: 9pt, fill: rgb("#4b5563"))[
      Veterinario: #d.vet_name \
      Matrícula Profesional: #d.vet_registry (#d.country) \
      Fecha de emisión: #d.issued_at \
      Válida hasta: #d.valid_until
    ]
  ],
  image("qr.png", width: 32mm),
)

#v(8pt)
#line(length: 100%, stroke: 0.5pt + rgb("#cbd5e1"))
#v(4pt)

#grid(
  columns: (1fr, 1fr),
  gutter: 12pt,
  [
    #text(weight: "bold", size: 10pt)[Datos de la Mascota] \
    Nombre: #d.pet_name \
    Especie: #d.pet_species \
    ID: #text(size: 8pt)[#raw(d.pet_id)]
  ],
  [
    #text(weight: "bold", size: 10pt)[Tutor Responsable] \
    Tutor: #d.guardian_name \
    ID Tutor: #text(size: 8pt)[#raw(d.guardian_id)] \
    Dispensaciones / Refills permitidos: #str(d.refills_allowed)
  ],
)

#v(12pt)
#text(size: 12pt, weight: "bold", fill: rgb("#1e3a8a"))[Prescripción Médica]
#v(4pt)

#if d.has_items {
  table(
    columns: (2fr, 1fr, 1fr, 1fr, 2fr),
    stroke: 0.4pt + rgb("#e2e8f0"),
    fill: (x, y) => if y == 0 { rgb("#f1f5f9") } else { none },
    [*Medicamento*], [*Dosis*], [*Vía*], [*Frecuencia*], [*Instrucciones*],
    ..d.items.map(i => (
      [
        #text(weight: "bold")[#i.name] \
        #if i.presentation != "" [#text(size: 8pt, fill: rgb("#64748b"))[#i.presentation]]
        #if i.controlled [#text(size: 7.5pt, fill: rgb("#dc2626"), weight: "bold")[CONTROLADO]]
      ],
      [#str(i.dose_value) #i.dose_unit],
      [#i.route],
      [#i.frequency #if i.duration_days > 0 [ \ (#str(i.duration_days) días)]],
      [#if i.instructions != "" [#i.instructions] else [-]],
    )).flatten(),
  )
} else [
  #text(style: "italic")[Sin medicamentos prescritos.]
]

#v(16pt)

#if d.has_controlled [
  #rect(
    width: 100%,
    fill: rgb("#fef2f2"),
    stroke: 0.5pt + rgb("#f87171"),
    inset: 8pt,
    radius: 4pt,
  )[
    #text(size: 8.5pt, fill: rgb("#991b1b"), weight: "bold")[
      AVISO DE REGULACIÓN FARMACÉUTICA:
    ]
    #text(size: 8pt, fill: rgb("#7f1d1d"))[
      Esta receta contiene medicamentos de control especial veterinario. La dispensación exige retención o registro de la receta según la normativa sanitaria local (#d.country).
    ]
  ]
  #v(8pt)
]

#line(length: 100%, stroke: 0.5pt + rgb("#cbd5e1"))
#v(6pt)

#text(size: 8pt, fill: rgb("#64748b"))[
  Firma digital registrada: #d.signature_algorithm por #d.vet_name el #d.signature_at. \
  ID Receta: #raw(d.id)
]
