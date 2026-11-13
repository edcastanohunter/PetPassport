// Factura local de PetPassport (PP-F5-ARQ-003): contingencia cuando el proveedor
// fiscal lleva más de 24 h caído, y países sin factura electrónica obligatoria.
// Los datos llegan en data.json; todos los valores se muestran como texto.
#let d = json("data.json")

#set document(title: d.title + " " + d.number)
#set page(paper: "a4", margin: (x: 18mm, top: 20mm, bottom: 24mm))
#set text(lang: "es", size: 10pt)

#text(size: 18pt, weight: "bold", fill: rgb("#1e3a8a"))[#d.title]
#v(2pt)
#text(size: 11pt, weight: "bold")[#d.number]
#v(1pt)
#text(size: 9pt, fill: rgb("#4b5563"))[Fecha de emisión: #d.issued_at]

#if d.notice != "" [
  #v(6pt)
  #text(size: 9pt, weight: "bold", fill: rgb("#b45309"))[#d.notice]
]

#v(8pt)
#line(length: 100%, stroke: 0.5pt + rgb("#cbd5e1"))
#v(4pt)

#text(weight: "bold")[Cliente] \
#d.customer_name \
#d.customer_tax_id \
#d.customer_address \
#d.customer_email

#v(8pt)

#table(
  columns: (1fr, auto, auto),
  align: (left, right, right),
  stroke: 0.5pt + rgb("#cbd5e1"),
  [*Concepto*], [*Cantidad*], [*Importe*],
  ..d.lines.map(l => (l.description, l.quantity, l.amount)).flatten(),
)

#v(6pt)

#align(right)[
  Subtotal: #d.subtotal \
  #d.tax_label: #d.tax \
  #text(weight: "bold")[Total: #d.total #d.currency]
]

#if d.period != "" [
  #v(6pt)
  #text(size: 9pt, fill: rgb("#4b5563"))[Periodo facturado: #d.period]
]
