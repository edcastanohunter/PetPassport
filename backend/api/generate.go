// Package api contiene el contrato OpenAPI y el código generado a partir de él.
package api

//go:generate go tool oapi-codegen -config oapi-codegen.yaml openapi.yaml
//go:generate go tool oapi-codegen -config oapi-codegen-partner.yaml partner-v1.yaml
