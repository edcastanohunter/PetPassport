// Plantilla del PDF del carnet de vacunación (PP-F2-PRD-015, ARQ-010).
// Los datos llegan en data.json; todos los valores se muestran como texto.
#let d = json("data.json")

#set document(title: "Carnet de vacunación " + d.pet_name)
#set page(
  paper: "a4",
  margin: (x: 18mm, top: 20mm, bottom: 24mm),
  footer: context [
    #set text(size: 7.5pt, fill: rgb("#555555"))
    #grid(
      columns: (1fr, auto),
      align: (left + horizon, right + horizon),
      [Huella de contenido (SHA-256): #raw(d.content_hash) \ Verificación: #d.verify_url],
      [Página #counter(page).display("1 / 1", both: true)],
    )
  ],
)
#set text(lang: "es", size: 10pt)

#grid(
  columns: (1fr, 32mm),
  gutter: 8pt,
  [
    #text(size: 16pt, weight: "bold")[Carnet de vacunación]
    #v(2pt)
    #text(size: 12pt)[#d.pet_name (#d.species)]
    #v(2pt)
    #text(size: 9pt, fill: rgb("#555555"))[Versión del documento #str(d.document_version)]
  ],
  image("qr.png", width: 30mm),
)

#v(8pt)
#if d.has_entries {
  table(
    columns: (auto, auto, 2fr, 1fr, 1fr, auto),
    stroke: 0.4pt + rgb("#cccccc"),
    [*Fecha*], [*Tipo*], [*Producto*], [*Lote*], [*Dosis*], [*Estado*],
    ..d.entries.map(e => (e.date, e.kind, e.product, e.lot, e.dose, e.verification + " (" + e.source + ")")).flatten(),
  )
} else [
  Sin registros de vacunación.
]

#if d.next_due.len() > 0 {
  v(10pt)
  text(size: 11pt, weight: "bold")[Próximas dosis]
  table(
    columns: (2fr, auto, auto),
    stroke: 0.4pt + rgb("#cccccc"),
    ..d.next_due.map(n => (n.product, n.due, n.status)).flatten(),
  )
}

#v(10pt)
#text(size: 8pt, fill: rgb("#666666"))[
  Las entradas «No verificado» fueron declaradas por el tutor y no han sido confirmadas por una clínica.
  Escanee el código QR para comprobar que este documento coincide con el registro vigente.
]
