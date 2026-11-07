package ocr

import _ "embed"

// SchemaV1JSON contiene el esquema JSON para PrescriptionExtraction v1 (PP-F4-OCR-003).
//
//go:embed schema/prescription_extraction.v1.json
var SchemaV1JSON []byte

// PromptV1MD contiene las instrucciones y glosario del transcriptor Claude v1 (PP-F4-OCR-003).
//
//go:embed prompts/extract_prescription_v1.md
var PromptV1MD []byte
